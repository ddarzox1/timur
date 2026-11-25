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

// FileServer отдаёт загруженный файл (с диска или редиректом в S3). Права проверяет Handler.
type FileServer interface {
	Serve(w http.ResponseWriter, r *http.Request, orgID int64, name string) error
}

type Options struct {
	CookieSecure bool
	PricePerUser int
	// FilesOrigin — адрес внешнего хранилища (S3) для CSP; пусто, если файлы на диске.
	FilesOrigin string
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
		return h.withUser(f, domain.RoleWorker, domain.RoleInstructor, domain.RoleSectionHead, domain.RoleShopHead, domain.RoleHSE, domain.RoleAdmin)
	}
	backoffice := func(f hf) http.Handler {
		return h.withUser(f, domain.RoleInstructor, domain.RoleSectionHead, domain.RoleShopHead, domain.RoleHSE, domain.RoleAdmin)
	}
	orgwide := func(f hf) http.Handler { return h.withUser(f, domain.RoleHSE, domain.RoleAdmin) }
	assigner := func(f hf) http.Handler { return h.withUser(f, domain.RoleInstructor, domain.RoleHSE, domain.RoleAdmin) }
	admin := func(f hf) http.Handler { return h.withUser(f, domain.RoleAdmin) }

	m.Handle("GET /static/", http.FileServerFS(web.FS))
	m.Handle("GET /{$}", h.public(h.landing))
	m.Handle("POST /pilot", h.public(h.pilotRequest))
	m.Handle("GET /lang/{l}", h.public(h.setLang))
	m.Handle("GET /login", h.public(h.loginPage))
	m.Handle("POST /login", h.public(h.login))
	m.Handle("POST /logout", anyone(h.logout))
	m.Handle("GET /files/{org}/{name}", anyone(h.file))

	// Сотрудник (мобильный)
	m.Handle("GET /m", anyone(h.mHome))
	m.Handle("GET /m/inductions/{id}", anyone(h.mInduction))
	m.Handle("POST /m/inductions/{id}/egov", anyone(h.mStartEDS))
	m.Handle("GET /m/inductions/{id}/egov/{op}", anyone(h.mEDSStatus))
	m.Handle("POST /m/inductions/{id}/egov/{op}/fake", anyone(h.mEDSFake))
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
	m.Handle("POST /m/profile/password", anyone(h.mPasswordChange))

	// Инженер ОТ / админ
	m.Handle("GET /admin", backoffice(h.adminDashboard))
	m.Handle("GET /admin/hazards", orgwide(h.hazardList))
	m.Handle("GET /admin/hazards.csv", orgwide(h.hazardsCSV))
	m.Handle("POST /admin/hazards/bulk", orgwide(h.hazardsBulk))
	m.Handle("GET /admin/hazards/{id}", orgwide(h.hazardCard))
	m.Handle("POST /admin/hazards/{id}", orgwide(h.hazardUpdate))
	m.Handle("POST /admin/hazards/{id}/comment", orgwide(h.hazardComment))
	m.Handle("GET /admin/inductions", backoffice(h.inductionTemplates))
	m.Handle("GET /admin/inductions/new", orgwide(h.inductionEdit))
	m.Handle("GET /admin/inductions/{id}", backoffice(h.inductionEdit))
	m.Handle("POST /admin/inductions/save", orgwide(h.inductionSave))
	m.Handle("GET /admin/assign", assigner(h.assignForm))
	m.Handle("POST /admin/assign", assigner(h.assign))
	m.Handle("GET /admin/assignments", backoffice(h.assignments))
	m.Handle("POST /admin/assignments/{batch}/remind", assigner(h.remind))
	m.Handle("POST /admin/assignments/{id}/admit", backoffice(h.admit))
	m.Handle("POST /admin/assignments/{id}/knowledge", backoffice(h.knowledgeResult))
	m.Handle("GET /admin/assignments/{id}/ddc", backoffice(h.ddc))
	m.Handle("GET /admin/sessions/{batch}/kiosk", assigner(h.kiosk))
	m.Handle("POST /admin/sessions/{batch}/kiosk", assigner(h.kioskStart))
	m.Handle("GET /admin/sessions/{batch}/kiosk/{op}", assigner(h.kioskStatus))
	m.Handle("POST /admin/sessions/{batch}/kiosk/{op}/fake", assigner(h.kioskFake))
	m.Handle("GET /admin/journals", backoffice(h.journal))
	m.Handle("GET /admin/journals/print", backoffice(h.journalPrint))
	m.Handle("GET /admin/journals.csv", backoffice(h.journalCSV))
	m.Handle("GET /admin/checklists", orgwide(h.checklistTemplates))
	m.Handle("GET /admin/checklists/new", orgwide(h.checklistEdit))
	m.Handle("GET /admin/checklists/row", orgwide(h.checklistRow))
	m.Handle("GET /admin/checklists/{id}", orgwide(h.checklistEdit))
	m.Handle("POST /admin/checklists/save", orgwide(h.checklistSave))
	m.Handle("GET /admin/runs", orgwide(h.runs))
	m.Handle("GET /admin/runs/{id}", orgwide(h.run))
	m.Handle("GET /admin/people", backoffice(h.peopleList))
	m.Handle("GET /admin/people/new", orgwide(h.personEdit))
	m.Handle("GET /admin/people/template.csv", orgwide(h.peopleTemplate))
	m.Handle("POST /admin/people/import", orgwide(h.peopleImport))
	m.Handle("GET /admin/people/{id}", backoffice(h.personEdit))
	m.Handle("POST /admin/people/save", orgwide(h.personSave))
	m.Handle("POST /admin/people/{id}/password", orgwide(h.personPasswordReset))
	m.Handle("GET /admin/org", backoffice(h.org))
	m.Handle("POST /admin/org/departments", orgwide(h.departmentSave))
	m.Handle("POST /admin/org/departments/{id}/delete", orgwide(h.departmentDelete))
	m.Handle("GET /admin/org/locations/{id}", orgwide(h.locationEdit))
	m.Handle("POST /admin/org/locations", orgwide(h.locationSave))
	m.Handle("GET /admin/settings", admin(h.settings))
	m.Handle("POST /admin/settings", admin(h.settingsSave))
	return secureHeaders(h.opt.FilesOrigin)(limitBody(sameOrigin(m)))
}
