package sigex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qauipsiz/internal/usecase/induction"
)

func TestClientSigningFlow(t *testing.T) {
	var server *httptest.Server
	var second atomic.Bool
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/egovQr":
			json.NewEncoder(w).Encode(map[string]any{"expireAt": time.Now().Add(time.Minute).UnixMilli(), "qrCode": "qr",
				"eGovMobileLaunchLink": "egov://launch", "dataURLAuto": server.URL + "/data", "signURLAuto": server.URL + "/sign"})
		case r.URL.Path == "/data":
			var payload basicPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.DocumentsToSign) != 1 || payload.DocumentsToSign[0].Document.File.Data == "" {
				t.Errorf("неверный payload Basic API: %+v, %v", payload, err)
			}
			io.WriteString(w, `{}`)
		case r.URL.Path == "/sign":
			io.WriteString(w, `{"status":"SIGNED","documentsToSign":[{"document":{"file":{"data":"cms-original"}}}]}`)
		case r.URL.Path == "/api" && r.Method == http.MethodPost:
			var request createDocument
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if !request.Settings.StrictSignersRequirements || !request.Settings.SequentialSignersRequirements ||
				request.Settings.SignaturesLimit != 2 || len(request.Settings.SignersRequirements) != 2 ||
				request.Settings.SignersRequirements[0].IIN != "IIN900101300017" || len(request.Settings.Unique) != 1 {
				t.Errorf("не заданы строгие последовательные подписанты: %+v", request.Settings)
			}
			io.WriteString(w, `{"documentId":"doc-1","signId":41}`)
		case r.URL.Path == "/api/doc-1/data":
			b, _ := io.ReadAll(r.Body)
			if string(b) != "%PDF-test" || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("неверная загрузка PDF: %q %q", b, r.Header.Get("Content-Type"))
			}
			io.WriteString(w, `{}`)
		case r.URL.Path == "/api/doc-1" && r.Method == http.MethodPost:
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["signType"] != "cms" || request["signature"] != "cms-instructor" {
				t.Errorf("неверная вторая подпись: %+v, %v", request, err)
			}
			second.Store(true)
			io.WriteString(w, `{"documentId":"doc-1","signId":42}`)
		case r.URL.Path == "/api/doc-1" && r.Method == http.MethodGet:
			first := `{"userId":"IIN900101300017","businessId":"BIN123456789012",` +
				`"subject":"CN=Работник","subjectStructure":[{"oid":"2.5.4.3","value":"Работник Тест"}],` +
				`"serialNumber":"abc","certificate":"cert-base64","from":1700000000000,"until":1800000000000,"issuer":"NCA",` +
				`"signAlgorithm":"SHA256withRSA","keyStorage":"PKCS12","policyIds":["p1"],"keyUsages":["digitalSignature"],` +
				`"extKeyUsages":["clientAuth"],"storedAt":1700000001000,"signId":41,"tsp":{"valid":true},"ocsp":{"status":"good"}}`
			items := first
			if second.Load() {
				items += `,{"userId":"IIN900101300027","subjectStructure":{"2.5.4.3":"Инструктор Тест"},` +
					`"certificate":"cert-2","signId":42,"tsp":{"valid":true},"ocsp":{"status":"good"}}`
			}
			io.WriteString(w, `{"signatures":[`+items+`]}`)
		case r.URL.Path == "/api/doc-1/signature/41":
			if r.URL.Query().Get("signFormat") != "0" {
				t.Error("не запрошена обогащённая CMS")
			}
			io.WriteString(w, `{"signature":"cms-enriched"}`)
		case r.URL.Path == "/api/doc-1/signature/42":
			io.WriteString(w, `{"signature":"cms-instructor-enriched"}`)
		case r.URL.Path == "/api/doc-1/buildDDC":
			w.Header().Set("Content-Type", "application/pdf")
			io.WriteString(w, "%PDF-ddc")
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{URL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	op, err := client.Start(ctx, []byte("%PDF-test"), "Запись")
	if err != nil || op.QRCode != "qr" || op.DataURL == "" || op.SignURL == "" {
		t.Fatalf("Start: %+v, %v", op, err)
	}
	result, err := client.Poll(ctx, op, []byte("%PDF-test"))
	if err != nil || !result.Done || result.CMS != "cms-original" {
		t.Fatalf("Poll: %+v, %v", result, err)
	}
	registration, err := client.Register(ctx, induction.EDSRegisterInput{PDF: []byte("%PDF-test"), CMS: result.CMS,
		ExpectedIINs: []string{"900101300017", "900101300027"}})
	if err != nil {
		t.Fatal(err)
	}
	if registration.DocumentID != "doc-1" || registration.SignID != 41 || registration.EnrichedCMS != "cms-enriched" ||
		registration.Evidence.IIN != "IIN900101300017" || registration.Evidence.Name != "Работник Тест" ||
		!strings.Contains(string(registration.Evidence.OCSP), "good") {
		t.Fatalf("неполная регистрация: %+v", registration)
	}
	ddc, err := client.BuildDDC(ctx, registration.DocumentID, []byte("%PDF-test"))
	if err != nil || string(ddc) != "%PDF-ddc" {
		t.Fatalf("DDC: %q, %v", ddc, err)
	}
	secondRegistration, err := client.Register(ctx, induction.EDSRegisterInput{DocumentID: "doc-1", CMS: "cms-instructor"})
	if err != nil || secondRegistration.SignID != 42 || secondRegistration.Evidence.IIN != "IIN900101300027" ||
		secondRegistration.Evidence.Name != "Инструктор Тест" || secondRegistration.EnrichedCMS != "cms-instructor-enriched" {
		t.Fatalf("вторая подпись: %+v, %v", secondRegistration, err)
	}
}

func TestParseBasicSignatureCanceled(t *testing.T) {
	status, cms, err := parseBasicSignature([]byte(`{"status":"CANCELED"}`))
	if err != nil || status != "CANCELED" || cms != "" {
		t.Fatalf("%q %q %v", status, cms, err)
	}
}
