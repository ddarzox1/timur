package storage

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"qauipsiz/internal/domain"
)

func TestSave(t *testing.T) {
	s := Local{Dir: t.TempDir()}
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64))
	url, err := s.Save(7, domain.Upload{Name: "a.png", Size: int64(len(png)), Content: bytes.NewReader(png)}, "image/")
	if err != nil || !strings.HasPrefix(url, "/files/7/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("%s %v", url, err)
	}
	got, _ := os.ReadFile(s.Path(7, strings.TrimPrefix(url, "/files/7/")))
	if !bytes.Equal(got, png) {
		t.Fatal("содержимое файла")
	}

	var ve domain.ValidationError
	txt := []byte("hello")
	if _, err := s.Save(7, domain.Upload{Name: "x.jpg", Content: bytes.NewReader(txt)}, ""); !errors.As(err, &ve) {
		t.Fatal("текст под видом картинки")
	}
	pdf := []byte("%PDF-1.4 ...")
	if _, err := s.Save(7, domain.Upload{Name: "d.pdf", Content: bytes.NewReader(pdf)}, "image/"); !errors.As(err, &ve) {
		t.Fatal("PDF там, где нужна картинка")
	}
	if _, err := s.Save(7, domain.Upload{Size: MaxSize + 1, Content: bytes.NewReader(png)}, ""); !errors.As(err, &ve) {
		t.Fatal("слишком большой файл")
	}
	if p := s.Path(7, "../../etc/passwd"); strings.Contains(p, "..") {
		t.Fatalf("path traversal: %s", p)
	}
}
