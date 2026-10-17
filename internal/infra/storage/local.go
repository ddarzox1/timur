package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"qauipsiz/internal/domain"
)

// Local хранит файлы на диске в {Dir}/{org}/.
// На сервере на Dir нужно монтировать том, иначе файлы пропадут при деплое.
type Local struct{ Dir string }

func (s Local) Save(orgID int64, f domain.Upload, onlyType string) (string, error) {
	ext, _, err := validate(f, onlyType)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.Dir, fmt.Sprint(orgID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := newName(ext)
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, f.Content); err != nil {
		return "", err
	}
	return fileURL(orgID, name), nil
}

// Path — путь к файлу на диске; имя очищается от каталогов.
func (s Local) Path(orgID int64, name string) string {
	return filepath.Join(s.Dir, fmt.Sprint(orgID), filepath.Base(name))
}

// Serve отдаёт файл. Права (своя организация) уже проверены вызывающим.
func (s Local) Serve(w http.ResponseWriter, r *http.Request, orgID int64, name string) error {
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, s.Path(orgID, name))
	return nil
}
