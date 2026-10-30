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
	data["OpID"] = r.URL.Query().Get("op")
	if rd.Location != nil {
		data["Loc"] = *rd.Location
	}
	return render(w, r, "m_induction", data)
}

func (h *Handler) mStartEDS(w http.ResponseWriter, r *http.Request, u domain.User) error {
	op, err := h.inductions.StartMobile(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/m/inductions/%d?op=%s", pathID(r), op.ID))
}

func (h *Handler) mEDSStatus(w http.ResponseWriter, r *http.Request, u domain.User) error {
	op, err := h.inductions.SigningStatus(r.Context(), u, r.PathValue("op"))
	if err != nil {
		return err
	}
	return renderBlock(w, r, "m_induction", "edsStatus", map[string]any{"Op": op, "ID": pathID(r)})
}

func (h *Handler) mEDSFake(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := h.inductions.CompleteFake(r.PathValue("op"), u.IIN, u.Name); err != nil {
		return err
	}
	return h.mEDSStatus(w, r, u)
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
		InstructorID: formID(r, "instructor_id"), LocationID: formID(r, "location_id"), Reason: r.FormValue("reason"),
		Basis: r.FormValue("basis"), WorkType: r.FormValue("work_type"), Docs: r.FormValue("docs"), NotifySMS: r.FormValue("notify") == "sms",
		AllowVisitors: r.FormValue("allow_visitors") == "1",
	})
	if err != nil {
		return err
	}
	return redirect(w, r, "/admin/assignments?assigned="+strconv.Itoa(n))
}

func (h *Handler) kiosk(w http.ResponseWriter, r *http.Request, u domain.User) error {
	batch := r.PathValue("batch")
	state, err := h.inductions.KioskState(r.Context(), u, batch)
	if err != nil {
		return err
	}
	rows, err := h.inductions.BatchAssignments(r.Context(), u, batch)
	if err != nil {
		return err
	}
	return render(w, r, "kiosk", map[string]any{"Batch": batch, "State": state, "Rows": rows, "OpID": r.URL.Query().Get("op")})
}

func (h *Handler) kioskStart(w http.ResponseWriter, r *http.Request, u domain.User) error {
	op, err := h.inductions.StartKiosk(r.Context(), u, r.PathValue("batch"))
	if err != nil {
		return err
	}
	return redirect(w, r, "/admin/sessions/"+r.PathValue("batch")+"/kiosk?op="+op.ID)
}

func (h *Handler) kioskStatus(w http.ResponseWriter, r *http.Request, u domain.User) error {
	op, err := h.inductions.SigningStatus(r.Context(), u, r.PathValue("op"))
	if err != nil {
		return err
	}
	return renderBlock(w, r, "kiosk", "kioskStatus", map[string]any{"Op": op, "Batch": r.PathValue("batch")})
}

func (h *Handler) kioskFake(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm); err != nil {
		return err
	}
	if err := h.inductions.CompleteFake(r.PathValue("op"), r.FormValue("iin"), r.FormValue("name")); err != nil {
		return err
	}
	return h.kioskStatus(w, r, u)
}

func (h *Handler) ddc(w http.ResponseWriter, r *http.Request, u domain.User) error {
	b, err := h.inductions.DDC(r.Context(), u, r.PathValue("batch"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="induction-ddc.pdf"`)
	_, err = w.Write(b)
	return err
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
	cw.Write([]string{"Дата", "ФИО", "Год рождения", "Профессия, должность", "Вид инструктажа", "Причина", "Рабочее место", "Основание", "Вид работ", "Документы", "Инструктирующий", "ЭЦП инструктирующего", "ЭЦП инструктируемого", "ИИН (маска)", "SIGEX"})
	for _, x := range rows {
		by := ""
		if x.BirthYear != nil {
			by = strconv.Itoa(*x.BirthYear)
		}
		cw.Write([]string{fmtDate(x.SignedAt), x.UserName, by, x.Position, domain.KindLabels[x.Kind] + ": " + x.Title, x.Reason,
			x.WorkLocation, x.Basis, x.WorkType, x.Docs,
			strings.Trim(x.Instructor+", "+x.InstructorPos, ", "), x.InstructorSignerName + " " + fmtDateTime(x.InstructorSignedAt),
			x.SignerName + " " + fmtDateTime(x.SignedAt), maskIIN(x.SignerIIN), x.SIGEXID})
	}
	cw.Flush()
	return cw.Error()
}

func maskIIN(s string) string {
	if len(s) != 12 {
		return s
	}
	return "********" + s[8:]
}
