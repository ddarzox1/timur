package httpdelivery

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/auth"
)

func homeFor(u domain.User) string {
	if u.HasBackoffice() {
		return "/admin"
	}
	return "/m"
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if u.ID != 0 {
		return redirect(w, r, homeFor(u))
	}
	return render(w, r, "login", nil)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	token, u, err := h.auth.Login(r.Context(), r.FormValue("iin"), r.FormValue("password"))
	if err != nil {
		var ve domain.ValidationError
		if !errors.As(err, &ve) {
			return err
		}
		return render(w, r, "login", map[string]any{"IIN": domain.NormalizeIIN(r.FormValue("iin")), "Error": string(ve)})
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: h.opt.CookieSecure, MaxAge: int(auth.SessionTTL.Seconds())})
	return redirect(w, r, homeFor(u))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := h.auth.Logout(r.Context(), c.Value); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	return redirect(w, r, "/login")
}

func langOf(r *http.Request) string {
	if c, err := r.Cookie("lang"); err == nil && c.Value == "kk" {
		return "kk"
	}
	return "ru"
}

func (h *Handler) setLang(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	l := r.PathValue("l")
	if l != "kk" {
		l = "ru"
	}
	http.SetCookie(w, &http.Cookie{Name: "lang", Value: l, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, sameHostBack(r), http.StatusSeeOther)
	return nil
}

// sameHostBack returns the referring path only for this host (prevents open redirects).
func sameHostBack(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host != r.Host || !strings.HasPrefix(ref.Path, "/") || strings.HasPrefix(ref.Path, "//") {
		return "/"
	}
	return (&url.URL{Path: ref.Path, RawQuery: ref.RawQuery}).String()
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if r.PathValue("org") != strconv.FormatInt(u.OrgID, 10) {
		return domain.ErrNotFound
	}
	return h.files.Serve(w, r, u.OrgID, r.PathValue("name"))
}
