package httpdelivery

import (
	"fmt"
	"net/http"
	"strconv"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/checklist"
)

// --- сотрудник ---

func (h *Handler) mChecklists(w http.ResponseWriter, r *http.Request, u domain.User) error {
	ts, err := h.checklists.ForUser(r.Context(), u)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	return render(w, r, "m_checklists", map[string]any{"Templates": ts, "Done": q.Get("done"), "Created": q.Get("hazards")})
}

func (h *Handler) mChecklistRun(w http.ResponseWriter, r *http.Request, u domain.User) error {
	t, err := h.checklists.Template(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	return render(w, r, "m_checklist", map[string]any{"T": t})
}

// mChecklistSubmit: поля формы a{i} (ответ), c{i} (комментарий), p{i} (фото), h{i} (создать опасность),
// где i — индекс пункта в шаблоне.
func (h *Handler) mChecklistSubmit(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm*10); err != nil {
		return err
	}
	t, err := h.checklists.Template(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	answers := map[int]checklist.AnswerInput{}
	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()
	for i := range t.Items {
		k := strconv.Itoa(i)
		photo, done := upload(r, "p"+k)
		closers = append(closers, done)
		answers[i] = checklist.AnswerInput{Value: r.FormValue("a" + k), Comment: r.FormValue("c" + k), Photo: photo, Hazard: r.FormValue("h"+k) == "1"}
	}
	res, err := h.checklists.Submit(r.Context(), u, t.ID, answers, r.FormValue("signature"))
	if err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/checklists?done=%d&hazards=%d", res.Issues, res.HazardsCreated))
}

// --- шаблоны и результаты ---

func (h *Handler) checklistTemplates(w http.ResponseWriter, r *http.Request, u domain.User) error {
	ts, err := h.checklists.Templates(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "checklists", map[string]any{"Templates": ts})
}

func (h *Handler) checklistEdit(w http.ResponseWriter, r *http.Request, u domain.User) error {
	t := domain.ChecklistTemplate{Frequency: "daily", Items: []domain.ChecklistItem{{Type: "section", Text: "Раздел 1"}, {Type: "question", HazardOnNo: true}}}
	if id := pathID(r); id != 0 {
		var err error
		if t, err = h.checklists.Template(r.Context(), u, id); err != nil {
			return err
		}
	}
	locs, err := h.people.Locations(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "checklist_edit", map[string]any{"T": t, "Locs": locs})
}

func (h *Handler) checklistRow(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	typ := "question"
	if r.URL.Query().Get("type") == "section" {
		typ = "section"
	}
	return renderBlock(w, r, "checklist_edit", "row", map[string]any{"Type": typ, "HazardOnNo": typ == "question"})
}

func (h *Handler) checklistSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	types, texts, hz, cats := r.Form["item_type"], r.Form["item_text"], r.Form["item_hazard"], r.Form["item_cat"]
	if len(types) != len(texts) || len(types) != len(hz) || len(types) != len(cats) {
		return domain.Invalid("Форма чек-листа повреждена, обновите страницу")
	}
	t := domain.ChecklistTemplate{Title: r.FormValue("title"), LocationID: formID(r, "location_id"), Frequency: r.FormValue("frequency")}
	if id := formID(r, "id"); id != nil {
		t.ID = *id
	}
	for i := range types {
		t.Items = append(t.Items, domain.ChecklistItem{Type: types[i], Text: texts[i], HazardOnNo: hz[i] == "1", Category: cats[i]})
	}
	if err := h.checklists.SaveTemplate(r.Context(), u, t); err != nil {
		return err
	}
	return redirect(w, r, "/admin/checklists")
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request, u domain.User) error {
	rs, err := h.checklists.Runs(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "runs", map[string]any{"Runs": rs})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request, u domain.User) error {
	x, err := h.checklists.Run(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	return render(w, r, "run", map[string]any{"R": x})
}
