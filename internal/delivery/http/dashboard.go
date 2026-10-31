package httpdelivery

import (
	"errors"
	"net/http"
	"strconv"

	"qauipsiz/internal/domain"
)

func (h *Handler) adminDashboard(w http.ResponseWriter, r *http.Request, u domain.User) error {
	period, _ := strconv.Atoi(r.URL.Query().Get("period"))
	st, err := h.dashboard.Stats(r.Context(), u, period)
	if err != nil {
		return err
	}
	return render(w, r, "dashboard", map[string]any{"S": st, "K": st.KPI, "Dynamics": st.Dynamics,
		"Deps": st.Departments, "Recent": st.Recent, "Upcoming": st.Upcoming, "Activity": st.Activity,
		"Notifications": st.Notifications, "LineTotal": st.LineTotal, "LineCompleted": st.LineCompleted})
}

func (h *Handler) mHome(w http.ResponseWriter, r *http.Request, u domain.User) error {
	home, err := h.dashboard.WorkerHome(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "m_home", map[string]any{"Days": home.DaysWithoutInjury, "Pending": home.Pending, "Checklists": home.Checklists,
		"Open": home.Hazards.Open, "Fixed": home.Hazards.Fixed, "Mine": home.Hazards.AssignedToMe})
}

func (h *Handler) mProfile(w http.ResponseWriter, r *http.Request, u domain.User) error {
	return h.profilePage(w, r, u, nil)
}

func (h *Handler) profilePage(w http.ResponseWriter, r *http.Request, u domain.User, data map[string]any) error {
	signed, err := h.inductions.Signed(r.Context(), u)
	if err != nil {
		return err
	}
	if data == nil {
		data = map[string]any{}
	}
	data["Signed"] = signed
	return render(w, r, "m_profile", data)
}

func (h *Handler) mPasswordChange(w http.ResponseWriter, r *http.Request, u domain.User) error {
	err := h.auth.ChangePassword(r.Context(), u, r.FormValue("current_password"), r.FormValue("new_password"), r.FormValue("confirm_password"))
	if err != nil {
		var ve domain.ValidationError
		if !errors.As(err, &ve) {
			return err
		}
		return h.profilePage(w, r, u, map[string]any{"PasswordError": string(ve)})
	}
	return h.profilePage(w, r, u, map[string]any{"PasswordSaved": true})
}
