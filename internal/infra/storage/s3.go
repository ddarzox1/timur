package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"qauipsiz/internal/domain"
)

// S3 хранит файлы в приватном бакете S3-совместимого хранилища (PS Cloud, Yandex, AWS, SeaweedFS…)
// под ключом {org}/{имя}.
// Отдача — редиректом на ссылку с подписью, живущую PresignTTL: трафик идёт мимо нашего сервера,
// а проверка прав остаётся в приложении.
type S3 struct {
	client     *minio.Client
	bucket     string
	PresignTTL time.Duration
}

type S3Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey string
	UseSSL                                         bool
}

func NewS3(ctx context.Context, c S3Config) (*S3, error) {
	client, err := minio.New(c.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: c.UseSSL,
		Region: c.Region,
	})
	if err != nil {
		return nil, err
	}
	ok, err := client.BucketExists(ctx, c.Bucket)
	if err != nil {
		return nil, fmt.Errorf("S3: нет доступа к бакету %q: %w", c.Bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("S3: бакет %q не существует", c.Bucket)
	}
	return &S3{client: client, bucket: c.Bucket, PresignTTL: 5 * time.Minute}, nil
}

func key(orgID int64, name string) string { return fmt.Sprintf("%d/%s", orgID, path.Base(name)) }

func (s *S3) Save(orgID int64, f domain.Upload, onlyType string) (string, error) {
	ext, ct, err := validate(f, onlyType)
	if err != nil {
		return "", err
	}
	name := newName(ext)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := s.client.PutObject(ctx, s.bucket, key(orgID, name), f.Content, f.Size, minio.PutObjectOptions{ContentType: ct}); err != nil {
		return "", fmt.Errorf("S3: загрузка: %w", err)
	}
	return fileURL(orgID, name), nil
}

// Serve отвечает редиректом на подписанную ссылку. Права (своя организация) уже проверены вызывающим.
func (s *S3) Serve(w http.ResponseWriter, r *http.Request, orgID int64, name string) error {
	params := url.Values{
		"response-content-type":        {contentTypeOf(name)},
		"response-content-disposition": {"inline"},
	}
	u, err := s.client.PresignedGetObject(r.Context(), s.bucket, key(orgID, name), s.PresignTTL, params)
	if err != nil {
		return err
	}
	// Ссылка протухает — кэшировать редирект нельзя.
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
	return nil
}

// Origin — адрес хранилища для CSP (img-src, media-src).
func (s *S3) Origin() string {
	u := s.client.EndpointURL()
	return u.Scheme + "://" + u.Host
}
