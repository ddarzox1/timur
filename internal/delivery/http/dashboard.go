package httpdelivery

import (
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
	return render(w, r, "dashboard", map[string]any{"O": st.Org, "Days": st.DaysWithoutInjury,
		"OpenH": st.OpenHazards, "OverdueH": st.OverdueHazards, "TotalA": st.Assigned, "SignedA": st.Signed, "Runs7": st.RunsWeek,
		"Expiring": st.Expiring, "Weeks": st.Weeks, "MaxW": st.MaxWeek, "Cats": st.Categories, "MaxC": st.MaxCategory,
		"Deps": st.Departments, "OverdueHz": st.OverdueHazardList, "OverdueAs": st.OverdueAssignments, "Period": st.Period})
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
	signed, err := h.inductions.Signed(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "m_profile", map[string]any{"Signed": signed})
}
