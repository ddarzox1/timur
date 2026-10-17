// Package httpdelivery — HTTP-слой: маршруты, разбор форм, рендер шаблонов.
// Обработчики не содержат бизнес-правил — только вызывают usecase.
package httpdelivery

import (
	"net/http"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/auth"
	"qauipsiz/internal/usecase/checklist"
	"qauipsiz/internal/usecase/dashboard"
	"qauipsiz/internal/usecase/hazard"
	"qauipsiz/internal/usecase/induction"
	"qauipsiz/internal/usecase/people"
	"qauipsiz/internal/usecase/pilot"
	"qauipsiz/web"
)

// FileServer отдаёт путь к загруженному файлу организации.
type FileServer interface {
	Path(orgID int64, name string) string
}

type Options struct {
	CookieSecure bool
	PricePerUser int
}

type Handler struct {
	auth       *auth.Service
	hazards    *hazard.Service
	inductions *induction.Service
	checklists *checklist.Service
	people     *people.Service
	dashboard  *dashboard.Service
	pilot      *pilot.Service
	files      FileServer
	opt        Options
}

type Services struct {
	Auth       *auth.Service
	Hazards    *hazard.Service
	Inductions *induction.Service
	Checklists *checklist.Service
	People     *people.Service
	Dashboard  *dashboard.Service
	Pilot      *pilot.Service
}

func New(s Services, files FileServer, opt Options) *Handler {
	return &Handler{auth: s.Auth, hazards: s.Hazards, inductions: s.Inductions, checklists: s.Checklists,
		people: s.People, dashboard: s.Dashboard, pilot: s.Pilot, files: files, opt: opt}
}

func (h *Handler) Routes() http.Handler {
	m := http.NewServeMux()
	anyone := func(f hf) http.Handler {
		return h.withUser(f, domain.RoleWorker, domain.RoleHSE, domain.RoleAdmin)
	}
	staff := func(f hf) http.Handler { return h.withUser(f, domain.RoleHSE, domain.RoleAdmin) }
	admin := func(f hf) http.Handler { return h.withUser(f, domain.RoleAdmin) }

	m.Handle("GET /static/", http.FileServerFS(web.FS))
	m.Handle("GET /{$}", h.public(h.landing))
	m.Handle("POST /pilot", h.public(h.pilotRequest))
	m.Handle("GET /lang/{l}", h.public(h.setLang))
	m.Handle("GET /login", h.public(h.loginPage))
	m.Handle("POST /login/phone", h.public(h.loginPhone))
	m.Handle("POST /login/code", h.public(h.loginCode))
	m.Handle("POST /logout", anyone(h.logout))
	m.Handle("GET /files/{org}/{name}", anyone(h.file))

	// Сотрудник (мобильный)
	m.Handle("GET /m", anyone(h.mHome))
	m.Handle("GET /m/inductions/{id}", anyone(h.mInduction))
	m.Handle("POST /m/inductions/{id}/sign", anyone(h.mSign))
	m.Handle("GET /m/hazards", anyone(h.mHazards))
	m.Handle("GET /m/hazards/new", anyone(h.mHazardNew))
	m.Handle("POST /m/hazards", anyone(h.mHazardCreate))
	m.Handle("GET /m/hazards/{id}", anyone(h.mHazard))
	m.Handle("POST /m/hazards/{id}/start", anyone(h.mHazardStart))
	m.Handle("POST /m/hazards/{id}/fix", anyone(h.mHazardFix))
	m.Handle("POST /m/hazards/{id}/comment", anyone(h.hazardComment))
	m.Handle("GET /m/checklists", anyone(h.mChecklists))
	m.Handle("GET /m/checklists/{id}", anyone(h.mChecklistRun))
	m.Handle("POST /m/checklists/{id}", anyone(h.mChecklistSubmit))
	m.Handle("GET /m/profile", anyone(h.mProfile))

	// Инженер ОТ / админ
	m.Handle("GET /admin", staff(h.adminDashboard))
	m.Handle("GET /admin/hazards", staff(h.hazardList))
	m.Handle("GET /admin/hazards.csv", staff(h.hazardsCSV))
	m.Handle("POST /admin/hazards/bulk", staff(h.hazardsBulk))
	m.Handle("GET /admin/hazards/{id}", staff(h.hazardCard))
	m.Handle("POST /admin/hazards/{id}", staff(h.hazardUpdate))
	m.Handle("POST /admin/hazards/{id}/comment", staff(h.hazardComment))
	m.Handle("GET /admin/inductions", staff(h.inductionTemplates))
	m.Handle("GET /admin/inductions/new", staff(h.inductionEdit))
	m.Handle("GET /admin/inductions/{id}", staff(h.inductionEdit))
	m.Handle("POST /admin/inductions/save", staff(h.inductionSave))
	m.Handle("GET /admin/assign", staff(h.assignForm))
	m.Handle("POST /admin/assign", staff(h.assign))
	m.Handle("GET /admin/assignments", staff(h.assignments))
	m.Handle("POST /admin/assignments/{batch}/remind", staff(h.remind))
	m.Handle("GET /admin/journals", staff(h.journal))
	m.Handle("GET /admin/journals/print", staff(h.journalPrint))
	m.Handle("GET /admin/journals.csv", staff(h.journalCSV))
	m.Handle("GET /admin/checklists", staff(h.checklistTemplates))
	m.Handle("GET /admin/checklists/new", staff(h.checklistEdit))
	m.Handle("GET /admin/checklists/row", staff(h.checklistRow))
	m.Handle("GET /admin/checklists/{id}", staff(h.checklistEdit))
	m.Handle("POST /admin/checklists/save", staff(h.checklistSave))
	m.Handle("GET /admin/runs", staff(h.runs))
	m.Handle("GET /admin/runs/{id}", staff(h.run))
	m.Handle("GET /admin/people", staff(h.peopleList))
	m.Handle("GET /admin/people/new", staff(h.personEdit))
	m.Handle("GET /admin/people/template.csv", staff(h.peopleTemplate))
	m.Handle("POST /admin/people/import", staff(h.peopleImport))
	m.Handle("GET /admin/people/{id}", staff(h.personEdit))
	m.Handle("POST /admin/people/save", staff(h.personSave))
	m.Handle("GET /admin/org", staff(h.org))
	m.Handle("POST /admin/org/departments", staff(h.departmentSave))
	m.Handle("POST /admin/org/departments/{id}/delete", staff(h.departmentDelete))
	m.Handle("GET /admin/org/locations/{id}", staff(h.locationEdit))
	m.Handle("POST /admin/org/locations", staff(h.locationSave))
	m.Handle("GET /admin/settings", admin(h.settings))
	m.Handle("POST /admin/settings", admin(h.settingsSave))
	return secureHeaders(limitBody(sameOrigin(m)))
}
