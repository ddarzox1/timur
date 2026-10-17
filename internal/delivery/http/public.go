package httpdelivery

import (
	"errors"
	"net/http"

	"qauipsiz/internal/domain"
)

func (h *Handler) landing(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	return render(w, r, "landing", map[string]any{"Price": h.opt.PricePerUser})
}

func (h *Handler) pilotRequest(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	err := h.pilot.Request(r.Context(), r.FormValue("company"), r.FormValue("name"), r.FormValue("phone"), r.FormValue("employees"))
	var ve domain.ValidationError
	if errors.As(err, &ve) {
		return renderBlock(w, r, "landing", "pilot", map[string]any{"Error": string(ve)})
	}
	if err != nil {
		return err
	}
	return renderBlock(w, r, "landing", "pilot", map[string]any{"Sent": true})
}
