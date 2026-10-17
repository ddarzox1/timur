package httpdelivery

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/hazard"
)

// --- сотрудник ---

func (h *Handler) mHazards(w http.ResponseWriter, r *http.Request, u domain.User) error {
	closed := r.URL.Query().Get("tab") == "closed"
	hs, err := h.hazards.Mine(r.Context(), u, closed)
	if err != nil {
		return err
	}
	return render(w, r, "m_hazards", map[string]any{"Hazards": hs, "Closed": closed})
}

func (h *Handler) mHazardNew(w http.ResponseWriter, r *http.Request, u domain.User) error {
	locs, err := h.people.Locations(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "m_hazard_new", map[string]any{"Locs": locs})
}

func (h *Handler) mHazardCreate(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm*hazard.MaxPhotos); err != nil {
		return err
	}
	photos, done := uploads(r, "photos")
	defer done()
	id, err := h.hazards.Create(r.Context(), u, hazard.CreateInput{
		LocationID: formID(r, "location_id"), Place: r.FormValue("place"), Category: r.FormValue("category"),
		Risk: r.FormValue("risk"), Description: r.FormValue("description"), Anonymous: r.FormValue("anonymous") == "1", Photos: photos,
	})
	if err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/hazards/%d?created", id))
}

func (h *Handler) mHazard(w http.ResponseWriter, r *http.Request, u domain.User) error {
	d, err := h.hazards.Get(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	return render(w, r, "m_hazard", map[string]any{"H": d.Hazard, "Photos": d.Photos, "Events": d.Events,
		"IsResp": d.Hazard.IsResponsible(u), "Created": r.URL.Query().Has("created")})
}

func (h *Handler) mHazardStart(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := h.hazards.Start(r.Context(), u, pathID(r)); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/hazards/%d", pathID(r)))
}

func (h *Handler) mHazardFix(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm); err != nil {
		return err
	}
	photo, done := upload(r, "photo")
	defer done()
	if err := h.hazards.Fix(r.Context(), u, pathID(r), r.FormValue("comment"), photo); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/hazards/%d", pathID(r)))
}

// hazardComment обслуживает и мобильную, и админскую карточку.
func (h *Handler) hazardComment(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := h.hazards.Comment(r.Context(), u, pathID(r), r.FormValue("text")); err != nil {
		return err
	}
	prefix := "/m"
	if strings.HasPrefix(r.URL.Path, "/admin") {
		prefix = "/admin"
	}
	return redirect(w, r, fmt.Sprintf("%s/hazards/%d", prefix, pathID(r)))
}

// --- реестр ---

func hazardFilter(r *http.Request) domain.HazardFilter {
	q := r.URL.Query()
	return domain.HazardFilter{Status: q.Get("status"), Risk: q.Get("risk"), Category: q.Get("cat"),
		Location: q.Get("loc"), Responsible: q.Get("resp"), From: q.Get("from"), To: q.Get("to")}
}

func (h *Handler) hazardList(w http.ResponseWriter, r *http.Request, u domain.User) error {
	hs, err := h.hazards.List(r.Context(), u, hazardFilter(r))
	if err != nil {
		return err
	}
	locs, err := h.people.Locations(r.Context(), u)
	if err != nil {
		return err
	}
	staff, err := h.people.Staff(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "hazards", map[string]any{"Hazards": hs, "Locs": locs, "Staff": staff, "Q": r.URL.Query(),
		"CSV": "/admin/hazards.csv?" + r.URL.RawQuery})
}

func (h *Handler) hazardsCSV(w http.ResponseWriter, r *http.Request, u domain.User) error {
	hs, err := h.hazards.List(r.Context(), u, hazardFilter(r))
	if err != nil {
		return err
	}
	cw := startCSV(w, "hazards.csv")
	cw.Write([]string{"№", "Дата", "Категория", "Риск", "Объект", "Место", "Описание", "Автор", "Ответственный", "Срок", "Статус", "Корректирующее действие"})
	for _, x := range hs {
		c, _ := domain.CategoryByKey(x.Category)
		cw.Write([]string{strconv.FormatInt(x.ID, 10), fmtDate(x.CreatedAt), c.Label, domain.RiskLabels[x.Risk], x.Location, x.Place,
			x.Description, x.Author, x.Responsible, fmtDate(x.DueAt), domain.StatusLabels[x.Status], x.Action})
	}
	cw.Flush()
	return cw.Error()
}

func (h *Handler) hazardsBulk(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	if err := h.hazards.AssignMany(r.Context(), u, formIDs(r, "ids"), formID(r, "responsible_id")); err != nil {
		return err
	}
	return redirect(w, r, "/admin/hazards?"+url.Values{"status": {r.FormValue("back_status")}}.Encode())
}

func (h *Handler) hazardCard(w http.ResponseWriter, r *http.Request, u domain.User) error {
	d, err := h.hazards.Get(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	staff, err := h.people.Staff(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "hazard", map[string]any{"H": d.Hazard, "Photos": d.Photos, "Events": d.Events, "Staff": staff})
}

func (h *Handler) hazardUpdate(w http.ResponseWriter, r *http.Request, u domain.User) error {
	err := h.hazards.Update(r.Context(), u, pathID(r), hazard.UpdateInput{
		ResponsibleID: formID(r, "responsible_id"), DueAt: formDate(r, "due_at"),
		Risk: r.FormValue("risk"), Status: r.FormValue("status"), Action: r.FormValue("action"),
	})
	if err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/admin/hazards/%d", pathID(r)))
}
