package sigex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/induction"
)

const maxResponse = 32 << 20

type Config struct {
	URL, Cert, Key string
	HTTPClient     *http.Client
}

type Client struct {
	base string
	http *http.Client
}

func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if base == "" {
		return nil, errors.New("SIGEX_URL не задан")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("SIGEX_URL: %w", err)
	}
	if (cfg.Cert == "") != (cfg.Key == "") {
		return nil, errors.New("SIGEX_CERT и SIGEX_KEY должны быть заданы вместе")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.Cert != "" {
			certificate, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
			if err != nil {
				return nil, fmt.Errorf("сертификат SIGEX: %w", err)
			}
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
		}
		hc = &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	}
	return &Client{base: base, http: hc}, nil
}

type qrResponse struct {
	ExpireAt    int64  `json:"expireAt"`
	QRCode      string `json:"qrCode"`
	LaunchLink  string `json:"eGovMobileLaunchLink"`
	DataURL     string `json:"dataURL"`
	DataURLAuto string `json:"dataURLAuto"`
	SignURL     string `json:"signURL"`
	SignURLAuto string `json:"signURLAuto"`
}

func (c *Client) Start(ctx context.Context, _ []byte, label string) (induction.EDSOperation, error) {
	var out qrResponse
	if err := c.json(ctx, http.MethodPost, c.base+"/api/egovQr", map[string]string{"description": label}, &out); err != nil {
		return induction.EDSOperation{}, err
	}
	if out.DataURLAuto != "" {
		out.DataURL = out.DataURLAuto
	}
	if out.SignURLAuto != "" {
		out.SignURL = out.SignURLAuto
	}
	if out.DataURL == "" || out.SignURL == "" {
		return induction.EDSOperation{}, errors.New("SIGEX не вернул URL операции QR")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return induction.EDSOperation{}, err
	}
	expires := unixTime(out.ExpireAt)
	if expires.IsZero() {
		expires = time.Now().Add(10 * time.Minute)
	}
	return induction.EDSOperation{ID: hex.EncodeToString(b), QRCode: out.QRCode, LaunchLink: out.LaunchLink,
		DataURL: out.DataURL, SignURL: out.SignURL, ExpiresAt: expires}, nil
}

type basicDocument struct {
	ID         int    `json:"id"`
	SignMethod string `json:"signMethod"`
	NameRu     string `json:"nameRu"`
	NameKz     string `json:"nameKz"`
	NameEn     string `json:"nameEn"`
	Document   struct {
		File struct {
			Mime string `json:"mime"`
			Data string `json:"data"`
		} `json:"file"`
	} `json:"document"`
}

type basicPayload struct {
	SignMethod      string          `json:"signMethod"`
	DocumentsToSign []basicDocument `json:"documentsToSign"`
}

func (c *Client) Poll(ctx context.Context, op induction.EDSOperation, pdf []byte) (induction.EDSResult, error) {
	deadline := time.Now().Add(110 * time.Second)
	if op.ExpiresAt.Before(deadline) {
		deadline = op.ExpiresAt
	}
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	doc := basicDocument{ID: 1, SignMethod: "CMS_SIGN_ONLY", NameRu: "Запись инструктажа", NameKz: "Нұсқама жазбасы", NameEn: "Induction record"}
	doc.Document.File.Mime = "@file/pdf"
	doc.Document.File.Data = base64.StdEncoding.EncodeToString(pdf)
	payload := basicPayload{SignMethod: "CMS_SIGN_ONLY", DocumentsToSign: []basicDocument{doc}}

	type answer struct {
		body []byte
		err  error
	}
	dataCh, signCh := make(chan answer, 1), make(chan answer, 1)
	go func() {
		b, err := c.request(pollCtx, http.MethodPost, c.resolve(op.DataURL), payload, "application/json")
		dataCh <- answer{b, err}
	}()
	go func() {
		b, err := c.request(pollCtx, http.MethodGet, c.resolve(op.SignURL), nil, "")
		signCh <- answer{b, err}
	}()

	for {
		select {
		case <-pollCtx.Done():
			if errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
				return induction.EDSResult{}, nil
			}
			return induction.EDSResult{}, pollCtx.Err()
		case a := <-dataCh:
			if a.err != nil && pollCtx.Err() == nil {
				return induction.EDSResult{}, a.err
			}
			dataCh = nil
		case a := <-signCh:
			if a.err != nil {
				if errors.Is(a.err, context.DeadlineExceeded) {
					return induction.EDSResult{}, nil
				}
				return induction.EDSResult{}, a.err
			}
			status, cms, err := parseBasicSignature(a.body)
			if err != nil {
				return induction.EDSResult{}, err
			}
			if strings.EqualFold(status, "CANCELED") || strings.EqualFold(status, "CANCELLED") {
				return induction.EDSResult{Done: true, Error: "Пользователь отменил подписание"}, nil
			}
			if cms == "" {
				return induction.EDSResult{}, errors.New("SIGEX не вернул CMS подпись")
			}
			return induction.EDSResult{Done: true, CMS: cms}, nil
		}
	}
}

func parseBasicSignature(b []byte) (string, string, error) {
	var raw struct {
		Status          string          `json:"status"`
		DocumentsToSign []basicDocument `json:"documentsToSign"`
		Document        basicDocument   `json:"document"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", "", fmt.Errorf("ответ подписи SIGEX: %w", err)
	}
	if len(raw.DocumentsToSign) > 0 {
		return raw.Status, raw.DocumentsToSign[0].Document.File.Data, nil
	}
	return raw.Status, raw.Document.Document.File.Data, nil
}

type createDocument struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SignType    string `json:"signType"`
	Signature   string `json:"signature"`
	Settings    struct {
		StrictSignersRequirements     bool                `json:"strictSignersRequirements"`
		SequentialSignersRequirements bool                `json:"sequentialSignersRequirements"`
		SignaturesLimit               int                 `json:"signaturesLimit"`
		Unique                        []string            `json:"unique"`
		SignersRequirements           []signerRequirement `json:"signersRequirements"`
	} `json:"settings"`
}

type signerRequirement struct {
	IIN string `json:"iin"`
}

type signatureMetadata struct {
	UserID, BusinessID, Subject, Issuer, SerialNumber string
	Certificate                                       string
	Name, SignAlgorithm, KeyStorage                   string
	From, Until, StoredAt                             int64
	SignID                                            int64
	PolicyIDs, KeyUsages, ExtKeyUsages                []string
	TSP, OCSP                                         json.RawMessage
}

func (c *Client) Register(ctx context.Context, in induction.EDSRegisterInput) (induction.EDSRegistration, error) {
	if in.CMS == "" {
		return induction.EDSRegistration{}, errors.New("SIGEX: пустая CMS")
	}
	docID, signID := in.DocumentID, int64(0)
	if docID == "" {
		request := createDocument{Title: "Запись инструктажа", Description: "Подписание записи инструктажа", SignType: "cms", Signature: in.CMS}
		request.Settings.StrictSignersRequirements = true
		request.Settings.SequentialSignersRequirements = true
		request.Settings.SignaturesLimit = len(in.ExpectedIINs)
		request.Settings.Unique = []string{"iin"}
		for _, iin := range in.ExpectedIINs {
			request.Settings.SignersRequirements = append(request.Settings.SignersRequirements, signerRequirement{IIN: "IIN" + strings.TrimSpace(iin)})
		}
		body, err := c.request(ctx, http.MethodPost, c.base+"/api", request, "application/json")
		if err != nil {
			return induction.EDSRegistration{}, err
		}
		docID, signID = responseIDs(body)
		if docID == "" {
			return induction.EDSRegistration{}, errors.New("SIGEX не вернул идентификатор документа")
		}
		if _, err := c.request(ctx, http.MethodPost, c.base+"/api/"+url.PathEscape(docID)+"/data", in.PDF, "application/octet-stream"); err != nil {
			return induction.EDSRegistration{}, err
		}
	} else {
		body, err := c.request(ctx, http.MethodPost, c.base+"/api/"+url.PathEscape(docID),
			map[string]string{"signType": "cms", "signature": in.CMS}, "application/json")
		if err != nil {
			return induction.EDSRegistration{}, err
		}
		_, signID = responseIDs(body)
	}

	meta, err := c.signatureMetadata(ctx, docID, signID)
	if err != nil {
		return induction.EDSRegistration{}, err
	}
	if signID == 0 {
		signID = meta.SignID
	}
	var enriched struct {
		Signature string `json:"signature"`
	}
	if err := c.json(ctx, http.MethodGet, fmt.Sprintf("%s/api/%s/signature/%d?signFormat=0&cmsAsPem=false", c.base, url.PathEscape(docID), signID), nil, &enriched); err != nil {
		return induction.EDSRegistration{}, err
	}
	if enriched.Signature == "" {
		return induction.EDSRegistration{}, errors.New("SIGEX не вернул обогащённую CMS")
	}
	return induction.EDSRegistration{DocumentID: docID, SignID: signID, EnrichedCMS: enriched.Signature,
		Evidence: domain.SignatureEvidence{IIN: meta.UserID, BIN: meta.BusinessID, Name: meta.Name, Subject: meta.Subject,
			Issuer: meta.Issuer, SerialNumber: meta.SerialNumber, Certificate: meta.Certificate,
			SignAlgorithm: meta.SignAlgorithm, KeyStorage: meta.KeyStorage,
			CertificateFrom: meta.From, CertificateUntil: meta.Until, StoredAt: meta.StoredAt, PolicyIDs: meta.PolicyIDs,
			KeyUsages: meta.KeyUsages, ExtKeyUsages: meta.ExtKeyUsages, TSP: meta.TSP, OCSP: meta.OCSP}}, nil
}

func (c *Client) signatureMetadata(ctx context.Context, documentID string, wanted int64) (signatureMetadata, error) {
	body, err := c.request(ctx, http.MethodGet, c.base+"/api/"+url.PathEscape(documentID), nil, "")
	if err != nil {
		return signatureMetadata{}, err
	}
	var raw struct {
		Signatures []map[string]any `json:"signatures"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return signatureMetadata{}, fmt.Errorf("метаданные SIGEX: %w", err)
	}
	if len(raw.Signatures) == 0 {
		return signatureMetadata{}, errors.New("SIGEX не вернул метаданные подписи")
	}
	selected := raw.Signatures[len(raw.Signatures)-1]
	if wanted != 0 {
		found := false
		for _, item := range raw.Signatures {
			if anyInt(item["signId"]) == wanted {
				selected = item
				found = true
				break
			}
		}
		if !found {
			return signatureMetadata{}, fmt.Errorf("SIGEX не вернул метаданные подписи %d", wanted)
		}
	}
	m := signatureMetadata{UserID: anyString(selected["userId"]), BusinessID: anyString(selected["businessId"]),
		Subject: anyString(selected["subject"]), Issuer: anyString(selected["issuer"]), SerialNumber: anyString(selected["serialNumber"]),
		Certificate:   anyString(selected["certificate"]),
		SignAlgorithm: anyString(selected["signAlgorithm"]), KeyStorage: anyString(selected["keyStorage"]),
		From: anyInt(selected["from"]), Until: anyInt(selected["until"]), StoredAt: anyInt(selected["storedAt"]), SignID: anyInt(selected["signId"]),
		PolicyIDs: anyStrings(selected["policyIds"]), KeyUsages: anyStrings(selected["keyUsages"]), ExtKeyUsages: anyStrings(selected["extKeyUsages"]),
		TSP: anyJSON(selected["tsp"]), OCSP: anyJSON(selected["ocsp"])}
	m.Name = subjectName(selected["subjectStructure"])
	if m.SignID == 0 {
		return signatureMetadata{}, errors.New("SIGEX не вернул идентификатор в метаданных подписи")
	}
	return m, nil
}

func (c *Client) BuildDDC(ctx context.Context, documentID string, pdf []byte) ([]byte, error) {
	return c.request(ctx, http.MethodPost, c.base+"/api/"+url.PathEscape(documentID)+"/buildDDC?fileName=induction.pdf&language=ru", pdf, "application/octet-stream")
}

func (*Client) Complete(string, string, string) error {
	return errors.New("Complete доступен только в fake-режиме")
}

func (c *Client) json(ctx context.Context, method, endpoint string, in, out any) error {
	b, err := c.request(ctx, method, endpoint, in, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("JSON SIGEX: %w", err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any, contentType string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reader = bytes.NewReader(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SIGEX %s: %w", method, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxResponse {
		return nil, errors.New("ответ SIGEX слишком большой")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(b))
		if len(message) > 500 {
			message = message[:500]
		}
		return nil, fmt.Errorf("SIGEX %s: HTTP %d: %s", method, resp.StatusCode, message)
	}
	return b, nil
}

func (c *Client) resolve(v string) string {
	if u, err := url.Parse(v); err == nil && u.IsAbs() {
		return v
	}
	return c.base + "/" + strings.TrimLeft(v, "/")
}
func unixTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	if v > 1e12 {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}
func anyString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func anyInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}
func anyStrings(v any) []string {
	a, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, anyString(x))
	}
	return out
}
func anyJSON(v any) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
func subjectName(v any) string {
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"2.5.4.3", "CN", "cn"} {
			if s := anyString(m[key]); s != "" {
				return s
			}
		}
		if strings.EqualFold(anyString(m["oid"]), "2.5.4.3") {
			return anyString(m["value"])
		}
		for _, x := range m {
			if s := subjectName(x); s != "" {
				return s
			}
		}
	}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if s := subjectName(x); s != "" {
				return s
			}
		}
	}
	return ""
}
func responseIDs(b []byte) (string, int64) {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return "", 0
	}
	doc := anyString(m["documentId"])
	if doc == "" {
		doc = anyString(m["id"])
	}
	return doc, anyInt(m["signId"])
}

var _ induction.EDS = (*Client)(nil)
