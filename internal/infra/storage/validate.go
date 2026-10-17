// Package storage — хранение загруженных файлов: на диске (Local) или в S3-совместимом хранилище (S3).
// В обоих случаях URL файла — /files/{org}/{имя}, права проверяет приложение.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"qauipsiz/internal/domain"
)

const MaxSize = 25 << 20

var allowed = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "application/pdf": ".pdf",
}

// validate проверяет размер и тип файла по содержимому (не по расширению) и перематывает его в начало.
// onlyType — обязательный префикс типа, например "image/"; "" — любой разрешённый.
func validate(f domain.Upload, onlyType string) (ext, contentType string, err error) {
	if f.Size > MaxSize {
		return "", "", domain.Invalid("Файл больше 25 МБ")
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f.Content, head)
	ct := strings.SplitN(http.DetectContentType(head[:n]), ";", 2)[0]
	ext, ok := allowed[ct]
	if !ok || !strings.HasPrefix(ct, onlyType) {
		return "", "", domain.Invalid("Неподдерживаемый тип файла: " + f.Name)
	}
	if _, err := f.Content.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	return ext, ct, nil
}

func newName(ext string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b) + ext
}

func fileURL(orgID int64, name string) string { return fmt.Sprintf("/files/%d/%s", orgID, name) }

// contentTypeOf — тип по расширению сохранённого файла (имена задаёт только сервер).
func contentTypeOf(name string) string {
	for ct, ext := range allowed {
		if strings.HasSuffix(name, ext) {
			return ct
		}
	}
	return "application/octet-stream"
}
