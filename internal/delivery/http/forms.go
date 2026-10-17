package httpdelivery

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/infra/storage"
)

// parseForm разбирает и multipart, и обычные формы.
func parseForm(r *http.Request, maxBytes int64) error {
	err := r.ParseMultipartForm(maxBytes)
	var tooBig *http.MaxBytesError
	switch {
	case err == nil, errors.Is(err, http.ErrNotMultipart):
		return nil
	case errors.As(err, &tooBig):
		return err
	default:
		return domain.Invalid("Не удалось прочитать форму")
	}
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func formID(r *http.Request, k string) *int64 {
	id, err := strconv.ParseInt(r.FormValue(k), 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	return &id
}

func formIDs(r *http.Request, k string) []int64 {
	var ids []int64
	for _, s := range r.Form[k] {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func formInt(r *http.Request, k string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(k)))
	if err != nil {
		return def
	}
	return n
}

func formDate(r *http.Request, k string) *time.Time {
	d, err := time.Parse(time.DateOnly, r.FormValue(k))
	if err != nil {
		return nil
	}
	return &d
}

func formFloat(r *http.Request, k string) *float64 {
	f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue(k)), ",", "."), 64)
	if err != nil {
		return nil
	}
	return &f
}

// uploads открывает непустые файлы поля. close() нужно вызвать после usecase.
func uploads(r *http.Request, key string) ([]domain.Upload, func()) {
	var out []domain.Upload
	var files []io.Closer
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File[key] {
			if fh.Size == 0 {
				continue
			}
			f, err := fh.Open()
			if err != nil {
				continue
			}
			files = append(files, f)
			out = append(out, domain.Upload{Name: fh.Filename, Size: fh.Size, Content: f.(multipart.File)})
		}
	}
	return out, func() {
		for _, f := range files {
			f.Close()
		}
	}
}

func upload(r *http.Request, key string) (*domain.Upload, func()) {
	us, done := uploads(r, key)
	if len(us) == 0 {
		return nil, done
	}
	return &us[0], done
}

const maxForm = storage.MaxSize
