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
	if u.IsStaff() {
		return "/admin"
	}
	return "/m"
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if u.ID != 0 {
		return redirect(w, r, homeFor(u))
	}
	return render(w, r, "login", map[string]any{"Step": "phone"})
}

// Ошибки валидации на шагах входа показываем внутри формы, а не всплывающим сообщением.
func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request, data map[string]any, err error) error {
	var ve domain.ValidationError
	if err != nil && !errors.As(err, &ve) {
		return err
	}
	if err != nil {
		data["Error"] = string(ve)
	}
	data["Fixed"] = h.auth.FixedCode()
	return renderBlock(w, r, "login", "form", data)
}

func (h *Handler) loginPhone(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	res, err := h.auth.RequestCode(r.Context(), r.FormValue("phone"))
	if err != nil {
		return h.loginForm(w, r, map[string]any{"Step": "phone", "Phone": r.FormValue("phone")}, err)
	}
	return h.loginForm(w, r, map[string]any{"Step": "code", "Phone": res.Phone, "Wait": res.Wait}, nil)
}

func (h *Handler) loginCode(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	token, u, err := h.auth.VerifyCode(r.Context(), r.FormValue("phone"), r.FormValue("code"))
	if err != nil {
		return h.loginForm(w, r, map[string]any{"Step": "code", "Phone": domain.NormalizePhone(r.FormValue("phone"))}, err)
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

// sameHostBack — путь, откуда пришёл пользователь, только если это наш сайт (иначе open redirect).
func sameHostBack(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host != r.Host || !strings.HasPrefix(ref.Path, "/") || strings.HasPrefix(ref.Path, "//") {
		return "/"
	}
	return (&url.URL{Path: ref.Path, RawQuery: ref.RawQuery}).String()
}

// file отдаёт загрузки только сотрудникам той же организации.
func (h *Handler) file(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if r.PathValue("org") != strconv.FormatInt(u.OrgID, 10) {
		return domain.ErrNotFound
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, h.files.Path(u.OrgID, r.PathValue("name")))
	return nil
}
