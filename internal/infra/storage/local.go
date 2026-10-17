// Package storage — хранение загруженных файлов.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"qauipsiz/internal/domain"
)

const MaxSize = 25 << 20

var allowed = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "application/pdf": ".pdf",
}

// Local хранит файлы на диске в {Dir}/{org}/ и отдаёт их по URL /files/{org}/{name}.
// ponytail: локальный диск; S3-совместимое хранилище — когда будет больше одного сервера.
type Local struct{ Dir string }

// Save проверяет тип по содержимому (не по расширению). onlyType — префикс, например "image/".
func (s Local) Save(orgID int64, f domain.Upload, onlyType string) (string, error) {
	if f.Size > MaxSize {
		return "", domain.Invalid("Файл больше 25 МБ")
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f.Content, head)
	ct := strings.SplitN(http.DetectContentType(head[:n]), ";", 2)[0]
	ext, ok := allowed[ct]
	if !ok || !strings.HasPrefix(ct, onlyType) {
		return "", domain.Invalid("Неподдерживаемый тип файла: " + f.Name)
	}
	if _, err := f.Content.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	dir := filepath.Join(s.Dir, fmt.Sprint(orgID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b := make([]byte, 16)
	rand.Read(b)
	name := hex.EncodeToString(b) + ext
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, f.Content); err != nil {
		return "", err
	}
	return fmt.Sprintf("/files/%d/%s", orgID, name), nil
}

// Path — путь к файлу организации на диске (имя очищается от каталогов).
func (s Local) Path(orgID int64, name string) string {
	return filepath.Join(s.Dir, fmt.Sprint(orgID), filepath.Base(name))
}
