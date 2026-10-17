package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"qauipsiz/internal/domain"
)

// Интеграционный тест с локальным S3: docker compose --profile s3 up -d s3 && docker compose --profile s3 run --rm s3-init
// Адрес можно задать через S3_TEST_ENDPOINT; без доступного S3 тест пропускается.
func testS3(t *testing.T) *S3 {
	t.Helper()
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9000"
	}
	s, err := NewS3(context.Background(), S3Config{Endpoint: endpoint, Bucket: "qauipsiz", AccessKey: "qauipsiz", SecretKey: "qauipsiz-secret"})
	if err != nil {
		t.Skip("нет локального S3: ", err)
	}
	return s
}

func TestS3SaveAndServe(t *testing.T) {
	s := testS3(t)
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x01", 128))
	url, err := s.Save(42, domain.Upload{Name: "a.png", Size: int64(len(png)), Content: bytes.NewReader(png)}, "image/")
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(url, "/files/42/")
	if !strings.HasPrefix(url, "/files/42/") || !strings.HasSuffix(name, ".png") {
		t.Fatalf("URL файла: %s", url)
	}
	info, err := s.client.StatObject(context.Background(), s.bucket, "42/"+name, minio.StatObjectOptions{})
	if err != nil || info.ContentType != "image/png" || info.Size != int64(len(png)) {
		t.Fatalf("объект в бакете: %+v %v", info, err)
	}

	rec := httptest.NewRecorder()
	if err := s.Serve(rec, httptest.NewRequest(http.MethodGet, url, nil), 42, "../../"+name); err != nil {
		t.Fatal(err)
	}
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.Contains(loc, "/qauipsiz/42/"+name) || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("редирект: %d %s %q", rec.Code, loc, rec.Header().Get("Cache-Control"))
	}
	res, err := http.Get(loc)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !bytes.Equal(got, png) || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("скачивание по ссылке: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// Тот же ключ без подписи не отдаётся: бакет приватный.
	plain := strings.SplitN(loc, "?", 2)[0]
	if res, err := http.Get(plain); err == nil && res.StatusCode == 200 {
		t.Fatal("бакет не должен быть публичным")
	}

	var ve domain.ValidationError
	if _, err := s.Save(42, domain.Upload{Name: "x.png", Content: bytes.NewReader([]byte("<html>"))}, "image/"); !errors.As(err, &ve) {
		t.Fatal("HTML под видом картинки")
	}
}
