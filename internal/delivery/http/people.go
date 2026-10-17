package httpdelivery

import (
	"net/http"
	"net/url"
	"strconv"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/people"
)

func (h *Handler) peopleList(w http.ResponseWriter, r *http.Request, u domain.User) error {
	q := r.URL.Query()
	ppl, err := h.people.List(r.Context(), u, domain.PeopleFilter{Query: q.Get("q"), Department: q.Get("dept"), Location: q.Get("loc")})
	if err != nil {
		return err
	}
	deps, err := h.people.Departments(r.Context(), u)
	if err != nil {
		return err
	}
	locs, err := h.people.Locations(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "people", map[string]any{"People": ppl, "Deps": deps, "Locs": locs, "Q": q, "Imported": q.Get("imported"), "Errors": q["err"]})
}

func (h *Handler) personEdit(w http.ResponseWriter, r *http.Request, u domain.User) error {
	ctx := r.Context()
	data := map[string]any{"P": domain.User{Role: domain.RoleWorker, Active: true}}
	if id := pathID(r); id != 0 {
		p, err := h.people.Get(ctx, u, id)
		if err != nil {
			return err
		}
		data["P"] = p
		if data["Inductions"], err = h.inductions.UserHistory(ctx, u, id); err != nil {
			return err
		}
		staffView := domain.User{ID: id, OrgID: u.OrgID, Role: domain.RoleWorker}
		mine, err := h.hazards.Mine(ctx, staffView, false)
		if err != nil {
			return err
		}
		closed, err := h.hazards.Mine(ctx, staffView, true)
		if err != nil {
			return err
		}
		data["Hazards"] = append(mine, closed...)
	}
	deps, err := h.people.Departments(ctx, u)
	if err != nil {
		return err
	}
	locs, err := h.people.Locations(ctx, u)
	if err != nil {
		return err
	}
	data["Deps"], data["Locs"] = deps, locs
	return render(w, r, "person", data)
}

func (h *Handler) personSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	p := domain.User{Name: r.FormValue("name"), Phone: r.FormValue("phone"), Position: r.FormValue("position"),
		DepartmentID: formID(r, "department_id"), LocationID: formID(r, "location_id"), Role: r.FormValue("role"),
		Active: r.FormValue("active") == "1"}
	if id := formID(r, "id"); id != nil {
		p.ID = *id
	}
	if y := formInt(r, "birth_year", 0); y > 0 {
		p.BirthYear = &y
	}
	if err := h.people.Save(r.Context(), u, p); err != nil {
		return err
	}
	return redirect(w, r, "/admin/people")
}

func (h *Handler) peopleTemplate(w http.ResponseWriter, r *http.Request, _ domain.User) error {
	cw := startCSV(w, "employees.csv")
	cw.Write(people.ImportHeader)
	cw.Write([]string{"Ерлан Сапаров", "+7 701 000 00 02", "1988", "Монтажник", "Строительный участок №1", "Объект ЖК Алатау, Алматы"})
	cw.Flush()
	return cw.Error()
}

func (h *Handler) peopleImport(w http.ResponseWriter, r *http.Request, u domain.User) error {
	f, _, err := r.FormFile("file")
	if err != nil {
		return domain.Invalid("Выберите CSV-файл")
	}
	defer f.Close()
	res, err := h.people.Import(r.Context(), u, f)
	if err != nil {
		return err
	}
	q := url.Values{"imported": {strconv.Itoa(res.Imported)}, "err": res.Errors}
	return redirect(w, r, "/admin/people?"+q.Encode())
}

// --- оргструктура ---

func (h *Handler) orgPage(w http.ResponseWriter, r *http.Request, u domain.User, edit domain.Location) error {
	deps, err := h.people.Departments(r.Context(), u)
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
	return render(w, r, "org", map[string]any{"Deps": deps, "Locs": locs, "Staff": staff, "L": edit})
}

func (h *Handler) org(w http.ResponseWriter, r *http.Request, u domain.User) error {
	return h.orgPage(w, r, u, domain.Location{RadiusM: 300})
}

func (h *Handler) locationEdit(w http.ResponseWriter, r *http.Request, u domain.User) error {
	l, err := h.people.Location(r.Context(), u, pathID(r))
	if err != nil {
		return err
	}
	return h.orgPage(w, r, u, l)
}

func (h *Handler) departmentSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	d := domain.Department{Name: r.FormValue("name"), ParentID: formID(r, "parent_id")}
	if id := formID(r, "id"); id != nil {
		d.ID = *id
	}
	if err := h.people.SaveDepartment(r.Context(), u, d); err != nil {
		return err
	}
	return redirect(w, r, "/admin/org")
}

func (h *Handler) departmentDelete(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := h.people.DeleteDepartment(r.Context(), u, pathID(r)); err != nil {
		return err
	}
	return redirect(w, r, "/admin/org")
}

func (h *Handler) locationSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	if err := parseForm(r, maxForm); err != nil {
		return err
	}
	evac, done := upload(r, "evacuation")
	defer done()
	l := domain.Location{Name: r.FormValue("name"), Address: r.FormValue("address"), Lat: formFloat(r, "lat"), Lng: formFloat(r, "lng"),
		RadiusM: formInt(r, "radius_m", 300), ResponsibleID: formID(r, "responsible_id")}
	if id := formID(r, "id"); id != nil {
		l.ID = *id
	}
	if err := h.people.SaveLocation(r.Context(), u, l, evac); err != nil {
		return err
	}
	return redirect(w, r, "/admin/org")
}

// --- настройки ---

func (h *Handler) settings(w http.ResponseWriter, r *http.Request, u domain.User) error {
	s, err := h.people.Settings(r.Context(), u)
	if err != nil {
		return err
	}
	return render(w, r, "settings", map[string]any{"O": s.Org, "Active": s.Active, "Price": h.opt.PricePerUser,
		"Total": s.Active * h.opt.PricePerUser, "Saved": r.URL.Query().Has("saved")})
}

func (h *Handler) settingsSave(w http.ResponseWriter, r *http.Request, u domain.User) error {
	o := domain.Org{Name: r.FormValue("name"), BIN: r.FormValue("bin"), LastInjuryAt: formDate(r, "last_injury_at"),
		RepeatWarnDays: formInt(r, "repeat_warn_days", 14)}
	if err := h.people.SaveSettings(r.Context(), u, o); err != nil {
		return err
	}
	return redirect(w, r, "/admin/settings?saved")
}
