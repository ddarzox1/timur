package httpdelivery

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/infra/storage"
)

// hf — обработчик с текущим пользователем. Ошибки превращаются в ответ в одном месте — serve.
type hf func(w http.ResponseWriter, r *http.Request, u domain.User) error

type ctxKey struct{}

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

const sessionCookie = "sid"

func (h *Handler) sessionUser(r *http.Request) *domain.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	u, err := h.auth.SessionUser(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return &u
}

// withUser требует вход и одну из ролей.
func (h *Handler) withUser(f hf, roles ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := h.sessionUser(r)
		if u == nil {
			redirect(w, r, "/login")
			return
		}
		if !slices.Contains(roles, u.Role) {
			http.Error(w, "Нет доступа", http.StatusForbidden)
			return
		}
		serve(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)), *u, f)
	})
}

// public — страница без обязательного входа; пользователь подставляется, если он есть.
func (h *Handler) public(f hf) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u domain.User
		if p := h.sessionUser(r); p != nil {
			u = *p
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
		}
		serve(w, r, u, f)
	})
}

func serve(w http.ResponseWriter, r *http.Request, u domain.User, f hf) {
	err := f(w, r, u)
	var ve domain.ValidationError
	var tooBig *http.MaxBytesError
	switch {
	case err == nil:
	case errors.As(err, &tooBig):
		http.Error(w, "Слишком большой запрос", http.StatusRequestEntityTooLarge)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "Не найдено", http.StatusNotFound)
	case errors.Is(err, domain.ErrForbidden):
		http.Error(w, "Нет доступа", http.StatusForbidden)
	case errors.As(err, &ve):
		http.Error(w, string(ve), http.StatusBadRequest)
	default:
		slog.Error("request", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "Внутренняя ошибка", http.StatusInternalServerError)
	}
}

// sameOrigin режет кросс-сайтовые POST; вместе с SameSite=Lax это защита от CSRF.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Лимиты тела запроса: multipart — до 5 файлов по 25 МБ, обычные формы — 1 МБ.
// Без MaxBytesReader ParseMultipartForm ограничивает только память и пишет остальное на диск без предела.
const (
	maxMultipartBody = 5*storage.MaxSize + 5<<20
	maxFormBody      = 1 << 20
)

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxFormBody)
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			limit = maxMultipartBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// csp: inline-скрипты и стили пока нужны (htmx-атрибуты, таймер входа, Sortable в конструкторе чек-листа).
const csp = "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; " +
	"frame-ancestors 'none'; form-action 'self'; base-uri 'self'; object-src 'none'"

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(self), camera=(self), microphone=()")
		if strings.HasPrefix(r.URL.Path, "/files/") {
			// Загруженный файл никогда не исполняется как страница, даже если внутри HTML.
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'")
		} else {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

// redirect работает и для обычных запросов, и для htmx.
func redirect(w http.ResponseWriter, r *http.Request, to string) error {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		return nil
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
	return nil
}
