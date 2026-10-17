package httpdelivery

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/induction"
)

// --- сотрудник ---

func (h *Handler) mInduction(w http.ResponseWriter, r *http.Request, u domain.User) error {
	rd, err := h.inductions.Open(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	data := map[string]any{"A": rd.Assignment, "T": rd.Template}
	if rd.Location != nil {
		data["Loc"] = *rd.Location
	}
	return render(w, r, "m_induction", data)
}

func (h *Handler) mSign(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm); err != nil {
		return err
	}
	selfie, done := upload(r, "selfie")
	defer done()
	err := h.inductions.Sign(r.Context(), u, pathID(r), induction.SignInput{
		Agree: r.FormValue("agree") == "1", Signature: r.FormValue("signature"), Selfie: selfie,
		Lat: formFloat(r, "lat"), Lng: formFloat(r, "lng"),
	})
	if err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/inductions/%d?done", pathID(r)))
}

// --- шаблоны ---

func (h *Handler) inductionTemplates(w http.ResponseWriter, r *http.Request, u domain.User) error {
	ts, err := h.inductions.Templates(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "inductions", map[string]any{"Templates": ts})
}

func (h *Handler) inductionEdit(w http.ResponseWriter, r *http.Request, u domain.User) error {
	t := domain.InductionTemplate{Kind: "primary"}
	if id := pathID(r); id != 0 {
		var err error
		if t, err = h.inductions.Template(r.Context(), u, id); err != nil {
			return err
		}
	}
	staff, err := h.people.Staff(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "induction_edit", map[string]any{"T": t, "Staff": staff})
}

func (h *Handler) inductionSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm); err != nil {
		return err
	}
	file, done := upload(r, "file")
	defer done()
	t := domain.InductionTemplate{
		Title: r.FormValue("title"), TitleKK: r.FormValue("title_kk"), Kind: r.FormValue("kind"),
		RepeatMonths: formInt(r, "repeat_months", 0), InstructorID: formID(r, "instructor_id"), Body: r.FormValue("body"),
		RequireGeo: r.FormValue("require_geo") == "1", RequireSelfie: r.FormValue("require_selfie") == "1",
	}
	if id := formID(r, "id"); id != nil {
		t.ID = *id
	}
	if err := h.inductions.SaveTemplate(r.Context(), u, t, file); err != nil {
		return err
	}
	return redirect(w, r, "/admin/inductions")
}

// --- назначения ---

func (h *Handler) assignForm(w http.ResponseWriter, r *http.Request, u domain.User) error {
	ctx, q := r.Context(), r.URL.Query()
	ts, err := h.inductions.Templates(ctx, u)
	if err != nil {
		return err
	}
	ppl, err := h.people.List(ctx, u, domain.PeopleFilter{Query: q.Get("q"), Department: q.Get("dept"), Location: q.Get("loc")})
	if err != nil {
		return err
	}
	active := ppl[:0]
	for _, p := range ppl {
		if p.Active {
			active = append(active, p)
		}
	}
	deps, err := h.people.Departments(ctx, u)
	if err != nil {
		return err
	}
	locs, err := h.people.Locations(ctx, u)
	if err != nil {
		return err
	}
	staff, err := h.people.Staff(ctx, u)
	if err != nil {
		return err
	}
	return render(w, r, "assign", map[string]any{"Templates": ts, "People": active, "Deps": deps, "Locs": locs, "Staff": staff,
		"Q": q, "TemplateID": q.Get("template"), "Due": domain.Today().AddDate(0, 0, 3).Format("2006-01-02")})
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	n, err := h.inductions.Assign(r.Context(), u, induction.AssignInput{
		TemplateID: formID(r, "template_id"), UserIDs: formIDs(r, "user_ids"), Due: formDate(r, "due_at"),
		InstructorID: formID(r, "instructor_id"), Reason: r.FormValue("reason"), NotifySMS: r.FormValue("notify") == "sms",
	})
	if err != nil {
		return err
	}
	return redirect(w, r, "/admin/assignments?assigned="+strconv.Itoa(n))
}

func (h *Handler) assignments(w http.ResponseWriter, r *http.Request, u domain.User) error {
	bs, err := h.inductions.Batches(r.Context(), u)
	if err != nil {
		return err
	}
	data := map[string]any{"Batches": bs, "Assigned": r.URL.Query().Get("assigned")}
	if b := r.URL.Query().Get("batch"); b != "" {
		if data["Rows"], err = h.inductions.BatchAssignments(r.Context(), u, b); err != nil {
			return err
		}
		data["Batch"] = b
	}
	return render(w, r, "assignments", data)
}

func (h *Handler) remind(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := h.inductions.Remind(r.Context(), u, r.PathValue("batch")); err != nil {
		return err
	}
	w.Write([]byte(`<span class="badge ok">Напоминания отправлены</span>`))
	return nil
}

// --- журналы ---

func journalFilter(r *http.Request) domain.JournalFilter {
	q := r.URL.Query()
	return domain.JournalFilter{Workplace: q.Get("journal") == "workplace", From: q.Get("from"), To: q.Get("to"),
		Department: q.Get("dept"), Name: strings.TrimSpace(q.Get("q"))}
}

func (h *Handler) journal(w http.ResponseWriter, r *http.Request, u domain.User) error {
	f := journalFilter(r)
	rows, err := h.inductions.Journal(r.Context(), u, f)
	if err != nil {
		return err
	}
	deps, err := h.people.Departments(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "journal", map[string]any{"Rows": rows, "Deps": deps, "Q": r.URL.Query(), "Workplace": f.Workplace})
}

func (h *Handler) journalPrint(w http.ResponseWriter, r *http.Request, u domain.User) error {
	f := journalFilter(r)
	rows, err := h.inductions.Journal(r.Context(), u, f)
	if err != nil {
		return err
	}
	return render(w, r, "journal_print", map[string]any{"Rows": rows, "Workplace": f.Workplace})
}

func (h *Handler) journalCSV(w http.ResponseWriter, r *http.Request, u domain.User) error {
	rows, err := h.inductions.Journal(r.Context(), u, journalFilter(r))
	if err != nil {
		return err
	}
	cw := startCSV(w, "journal.csv")
	cw.Write([]string{"Дата", "ФИО", "Год рождения", "Профессия, должность", "Вид инструктажа", "Причина", "Инструктирующий", "Подписано", "Геолокация"})
	for _, x := range rows {
		by, geo := "", ""
		if x.BirthYear != nil {
			by = strconv.Itoa(*x.BirthYear)
		}
		if x.GeoOK != nil {
			geo = map[bool]string{true: "на объекте", false: "вне объекта"}[*x.GeoOK]
		}
		cw.Write([]string{fmtDate(x.SignedAt), x.UserName, by, x.Position, domain.KindLabels[x.Kind] + ": " + x.Title, x.Reason,
			strings.Trim(x.Instructor+", "+x.InstructorPos, ", "), fmtDateTime(x.SignedAt), geo})
	}
	cw.Flush()
	return cw.Error()
}
