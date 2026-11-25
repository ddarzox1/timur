// Package induction — инструктажи: шаблоны, назначения, подпись сотрудника, журналы.
package induction

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"qauipsiz/internal/domain"
)

type Repo interface {
	InductionTemplates(ctx context.Context, orgID int64) ([]domain.InductionTemplate, error)
	InductionTemplate(ctx context.Context, orgID, id int64) (domain.InductionTemplate, error)
	SaveInductionTemplate(ctx context.Context, orgID int64, t domain.InductionTemplate) error
	// UserAssignment — назначение конкретного сотрудника (чужое → ErrNotFound).
	UserAssignment(ctx context.Context, userID, id int64) (domain.Assignment, error)
	// SignAssignment подписывает, только если ещё не подписано; false — уже подписано.
	SignAssignment(ctx context.Context, userID, id int64, s domain.Signature) (bool, error)
	// CreateAssignments создаёт назначения только для сотрудников этой организации; возвращает их число.
	UsersForAssignment(ctx context.Context, viewer domain.User, userIDs []int64) ([]domain.User, error)
	CreateAssignments(ctx context.Context, viewer domain.User, templateID int64, instructorID, locationID *int64,
		batch, reason, basis, workType, docs string, due time.Time, userIDs []int64, doc domain.InductionDoc) (int, error)
	BatchExists(ctx context.Context, viewer domain.User, batch string) (bool, error)
	UnsignedInBatch(ctx context.Context, orgID int64, batch string) ([]domain.Notice, error)
	Batches(ctx context.Context, viewer domain.User) ([]domain.Batch, error)
	BatchAssignments(ctx context.Context, viewer domain.User, batch string) ([]domain.Assignment, error)
	ScopedAssignment(ctx context.Context, viewer domain.User, id int64) (domain.Assignment, error)
	SaveKnowledgeRecord(ctx context.Context, viewer domain.User, id int64, result, pdfPath, sha256 string) error
	SnapshotBatchMaterial(ctx context.Context, viewer domain.User, batch, body, filePath, sha256 string) error
	ApplyAssignmentEDS(ctx context.Context, viewer domain.User, id int64, instructor bool, sig domain.EDSSignature) error
	CreateSigningOperation(ctx context.Context, op domain.SigningOperationRecord) error
	SigningOperation(ctx context.Context, orgID int64, id string) (domain.SigningOperationRecord, error)
	ClaimSigningOperation(ctx context.Context, orgID int64, id string) (domain.SigningOperationRecord, bool, error)
	SaveSigningOperation(ctx context.Context, op domain.SigningOperationRecord) error
	ReleaseSigningOperation(ctx context.Context, orgID int64, id string) error
	Journal(ctx context.Context, viewer domain.User, kinds []string, f domain.JournalFilter) ([]domain.Assignment, error)
	UserAssignments(ctx context.Context, userID int64, signed bool) ([]domain.Assignment, error)
	KioskState(ctx context.Context, viewer domain.User, batch string) (domain.KioskState, error)
	CanSignBatch(ctx context.Context, viewer domain.User, batch, iin string) error
	ApplyBatchEDS(ctx context.Context, viewer domain.User, batch string, sig domain.EDSSignature) error
	// AdmitAssignment — стажировка и допуск по подписанному первичному инструктажу в области viewer; иначе ErrNotFound.
	AdmitAssignment(ctx context.Context, viewer domain.User, id int64, in domain.AdmitInput, byName string) error
}

type Directory interface {
	Location(ctx context.Context, orgID, id int64) (domain.Location, error)
	UserName(ctx context.Context, orgID, id int64) (string, error)
	DirectoryUser(ctx context.Context, orgID, id int64) (domain.User, error)
	Org(ctx context.Context, id int64) (domain.Org, error)
}

type Storage interface {
	Save(orgID int64, f domain.Upload, onlyType string) (string, error)
	Read(orgID int64, name string) ([]byte, error)
}

type ProtocolBuilder interface {
	Build(domain.ProtocolInput) ([]byte, string, error)
}

type EDSOperation struct {
	ID, QRCode, LaunchLink, DataURL, SignURL string
	ExpiresAt                                time.Time
}

type EDSResult struct {
	Done           bool
	IIN, Name, CMS string
}

type EDS interface {
	Start(context.Context, []byte, string) (EDSOperation, error)
	Poll(context.Context, EDSOperation) (EDSResult, error)
	Register(context.Context, string, []byte, string) (string, int64, error)
	BuildDDC(context.Context, string, []byte) ([]byte, error)
	Complete(string, string, string) error
}

type SMSSender interface {
	Send(phone, text string) error
}

type Service struct {
	repo     Repo
	dir      Directory
	files    Storage
	sms      SMSSender
	protocol ProtocolBuilder
	eds      EDS
	baseURL  string
}

func New(repo Repo, dir Directory, files Storage, sms SMSSender, protocol ProtocolBuilder, eds EDS, baseURL string) *Service {
	return &Service{repo: repo, dir: dir, files: files, sms: sms, protocol: protocol, eds: eds, baseURL: strings.TrimRight(baseURL, "/")}
}

// --- сотрудник ---

type Reading struct {
	Assignment domain.Assignment
	Template   domain.InductionTemplate
	Location   *domain.Location
}

func (s *Service) Open(ctx context.Context, u domain.User, id int64) (Reading, error) {
	a, err := s.repo.UserAssignment(ctx, u.ID, id)
	if err != nil {
		return Reading{}, err
	}
	t, err := s.repo.InductionTemplate(ctx, u.OrgID, a.TemplateID)
	if err != nil {
		return Reading{}, err
	}
	t.Body, t.FilePath = a.MaterialBody, a.MaterialFilePath
	r := Reading{Assignment: a, Template: t}
	if u.LocationID != nil {
		if loc, err := s.dir.Location(ctx, u.OrgID, *u.LocationID); err == nil {
			r.Location = &loc
		}
	}
	return r, nil
}

type SignInput struct {
	Agree     bool
	Signature string
	Selfie    *domain.Upload
	Lat, Lng  *float64
}

// Sign — сотрудник подтверждает ознакомление и расписывается. Повторно подписать нельзя.
func (s *Service) Sign(ctx context.Context, u domain.User, id int64, in SignInput) error {
	r, err := s.Open(ctx, u, id)
	if err != nil {
		return err
	}
	if r.Assignment.SignedAt != nil {
		return domain.Invalid("Инструктаж уже подписан")
	}
	if !in.Agree {
		return domain.Invalid("Подтвердите, что ознакомились с инструктажем")
	}
	if !domain.ValidSignature(in.Signature) {
		return domain.Invalid("Поставьте подпись")
	}
	if r.Template.RequireSelfie && in.Selfie == nil {
		return domain.Invalid("Нужно селфи")
	}
	if r.Template.RequireGeo && (in.Lat == nil || in.Lng == nil) {
		return domain.Invalid("Разрешите доступ к геолокации")
	}
	sig := domain.Signature{Image: in.Signature, Lat: in.Lat, Lng: in.Lng}
	if in.Lat != nil && in.Lng != nil && r.Location != nil {
		if inside, ok := r.Location.Contains(*in.Lat, *in.Lng); ok {
			sig.GeoOK = &inside
		}
	}
	if in.Selfie != nil {
		if sig.Selfie, err = s.files.Save(u.OrgID, *in.Selfie, "image/"); err != nil {
			return err
		}
	}
	signed, err := s.repo.SignAssignment(ctx, u.ID, id, sig)
	if err != nil {
		return err
	}
	if !signed {
		return domain.Invalid("Инструктаж уже подписан")
	}
	return nil
}

func (s *Service) Pending(ctx context.Context, u domain.User) ([]domain.Assignment, error) {
	return s.repo.UserAssignments(ctx, u.ID, false)
}

func (s *Service) Signed(ctx context.Context, u domain.User) ([]domain.Assignment, error) {
	return s.repo.UserAssignments(ctx, u.ID, true)
}

// --- шаблоны ---

func (s *Service) Templates(ctx context.Context, u domain.User) ([]domain.InductionTemplate, error) {
	return s.repo.InductionTemplates(ctx, u.OrgID)
}

func (s *Service) Template(ctx context.Context, u domain.User, id int64) (domain.InductionTemplate, error) {
	return s.repo.InductionTemplate(ctx, u.OrgID, id)
}

// SaveTemplate создаёт (ID=0) или обновляет шаблон. file — новый PDF/видео, если загружен.
func (s *Service) SaveTemplate(ctx context.Context, u domain.User, t domain.InductionTemplate, file *domain.Upload) error {
	if !u.SeesOrg() {
		return domain.ErrForbidden
	}
	t.Title, t.TitleKK = domain.Clip(t.Title, domain.MaxTitle), domain.Clip(t.TitleKK, domain.MaxTitle)
	t.Body = domain.Clip(t.Body, domain.MaxBody)
	if t.Title == "" || domain.KindLabels[t.Kind] == "" {
		return domain.Invalid("Укажите название и вид инструктажа")
	}
	if t.RepeatMonths < 0 || t.RepeatMonths > 36 {
		return domain.Invalid("Неверная периодичность")
	}
	if t.Kind == "repeat" && t.RepeatMonths == 0 {
		t.RepeatMonths = 6
	}
	if t.InstructorID != nil {
		if _, err := s.dir.UserName(ctx, u.OrgID, *t.InstructorID); err != nil {
			return domain.Invalid("Неизвестный сотрудник")
		}
	}
	t.FilePath = "" // путь задаёт только загрузка; пустой — оставить прежний файл
	if file != nil {
		p, err := s.files.Save(u.OrgID, *file, "")
		if err != nil {
			return err
		}
		t.FilePath = p
	}
	return s.repo.SaveInductionTemplate(ctx, u.OrgID, t)
}

// --- назначения ---

type AssignInput struct {
	TemplateID    *int64
	UserIDs       []int64
	Due           *time.Time
	InstructorID  *int64
	Reason        string
	LocationID    *int64
	Basis         string
	WorkType      string
	Docs          string
	NotifySMS     bool
	AllowVisitors bool
}

// Assign назначает инструктаж группе сотрудников; возвращает число назначений.
func (s *Service) Assign(ctx context.Context, u domain.User, in AssignInput) (int, error) {
	if u.Role != domain.RoleInstructor && !u.SeesOrg() {
		return 0, domain.ErrForbidden
	}
	if in.TemplateID == nil || in.Due == nil || len(in.UserIDs) == 0 {
		return 0, domain.Invalid("Выберите инструктаж, сотрудников и срок")
	}
	t, err := s.repo.InductionTemplate(ctx, u.OrgID, *in.TemplateID)
	if err != nil {
		return 0, err
	}
	in.Reason = domain.Clip(in.Reason, domain.MaxTitle)
	in.Basis = domain.Clip(in.Basis, domain.MaxTitle)
	in.WorkType = domain.Clip(in.WorkType, domain.MaxTitle)
	in.Docs = domain.Clip(in.Docs, domain.MaxBody)
	if t.Kind == "unplanned" && in.Reason == "" {
		return 0, domain.Invalid("Для внепланового инструктажа укажите причину")
	}
	users, err := s.repo.UsersForAssignment(ctx, u, in.UserIDs)
	if err != nil {
		return 0, err
	}
	if len(users) != len(in.UserIDs) {
		return 0, domain.Invalid("Часть сотрудников недоступна в вашей области")
	}
	for _, person := range users {
		if !domain.ValidIIN(person.IIN) {
			return 0, domain.Invalid("Для подписи ЭЦП у каждого участника должен быть указан корректный ИИН")
		}
		if person.BirthYear == nil {
			return 0, domain.Invalid("Для журнала у каждого сотрудника должен быть указан год рождения")
		}
		if strings.TrimSpace(person.Position) == "" {
			return 0, domain.Invalid("Для журнала у каждого сотрудника должна быть указана профессия или должность")
		}
		if t.Kind == "intro" && person.DepartmentID == nil {
			return 0, domain.Invalid("Для вводного журнала у каждого сотрудника должно быть указано подразделение")
		}
	}
	if t.Kind == "primary" {
		if in.LocationID == nil {
			return 0, domain.Invalid("Для первичного инструктажа укажите рабочее место")
		}
	}
	if in.LocationID != nil {
		if _, err := s.dir.Location(ctx, u.OrgID, *in.LocationID); err != nil {
			return 0, domain.Invalid("Неизвестное рабочее место")
		}
	}
	if t.Kind == "target" && (in.Basis == "" || in.WorkType == "" || in.Docs == "") {
		return 0, domain.Invalid("Для целевого инструктажа укажите основание, вид работ и связанные документы")
	}
	instructor := t.InstructorID
	if in.InstructorID != nil {
		if _, err := s.dir.UserName(ctx, u.OrgID, *in.InstructorID); err != nil {
			return 0, domain.Invalid("Неизвестный сотрудник")
		}
		instructor = in.InstructorID
	}
	if instructor == nil {
		return 0, domain.Invalid("Укажите инструктирующего для подписания протокола ЭЦП")
	}
	instructorUser, err := s.dir.DirectoryUser(ctx, u.OrgID, *instructor)
	if err != nil {
		return 0, domain.Invalid("Неизвестный инструктирующий")
	}
	if !domain.ValidIIN(instructorUser.IIN) {
		return 0, domain.Invalid("У инструктирующего должен быть указан корректный ИИН")
	}
	if strings.TrimSpace(instructorUser.Position) == "" {
		return 0, domain.Invalid("У инструктирующего должна быть указана должность")
	}
	batch := newBatch()
	org, err := s.dir.Org(ctx, u.OrgID)
	if err != nil {
		return 0, err
	}
	location := ""
	if in.LocationID != nil {
		loc, err := s.dir.Location(ctx, u.OrgID, *in.LocationID)
		if err != nil {
			return 0, err
		}
		location = loc.Name
	}
	assignedAt := time.Now()
	materialHash := sha256.New()
	materialHash.Write([]byte(t.Body))
	if t.FilePath != "" {
		material, err := s.files.Read(u.OrgID, t.FilePath)
		if err != nil {
			return 0, err
		}
		materialHash.Write(material)
	}
	materialSHA := fmt.Sprintf("%x", materialHash.Sum(nil))
	pdfBytes, sum, err := s.protocol.Build(domain.ProtocolInput{Org: org, Template: t, Batch: batch, AssignedAt: assignedAt,
		DueAt: *in.Due, Instructor: instructorUser.Name, Location: location, Reason: in.Reason, Basis: in.Basis,
		WorkType: in.WorkType, Docs: in.Docs, MaterialSHA: materialSHA})
	if err != nil {
		return 0, err
	}
	pdfPath, err := s.files.Save(u.OrgID, domain.Upload{Name: "protocol.pdf", Size: int64(len(pdfBytes)), Content: bytes.NewReader(pdfBytes)}, "application/pdf")
	if err != nil {
		return 0, err
	}
	doc := domain.InductionDoc{OrgID: u.OrgID, Batch: batch, PDFPath: pdfPath, SHA256: sum, AllowVisitors: in.AllowVisitors}
	n, err := s.repo.CreateAssignments(ctx, u, t.ID, instructor, in.LocationID, batch, in.Reason, in.Basis, in.WorkType, in.Docs, *in.Due, in.UserIDs, doc)
	if err == nil {
		err = s.repo.SnapshotBatchMaterial(ctx, u, batch, t.Body, t.FilePath, materialSHA)
	}
	if err != nil || !in.NotifySMS {
		return n, err
	}
	return n, s.notify(ctx, u.OrgID, batch)
}

func newBatch() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) notify(ctx context.Context, orgID int64, batch string) error {
	ns, err := s.repo.UnsignedInBatch(ctx, orgID, batch)
	if err != nil {
		return err
	}
	for _, n := range ns {
		text := fmt.Sprintf("Qauipsiz: подпишите инструктаж «%s»: %s/m/inductions/%d", n.Title, s.baseURL, n.AssignmentID)
		if err := s.sms.Send(n.Phone, text); err != nil {
			return err
		}
	}
	return nil
}

// Remind — повторная SMS тем, кто ещё не подписал.
func (s *Service) Remind(ctx context.Context, u domain.User, batch string) error {
	if u.Role != domain.RoleInstructor && !u.SeesOrg() {
		return domain.ErrForbidden
	}
	ok, err := s.repo.BatchExists(ctx, u, batch)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrNotFound
	}
	return s.notify(ctx, u.OrgID, batch)
}

// Admit — стажировка и допуск к самостоятельной работе после первичного инструктажа.
// ponytail: допуск — запись авторизованного пользователя, не ЭЦП; перевести на подпись SIGEX
// (отдельный PDF допуска и подпись работника «стажировку прошёл»), когда SIGEX заработает в prod.
func (s *Service) Admit(ctx context.Context, u domain.User, id int64, in domain.AdmitInput) error {
	if u.Role != domain.RoleInstructor && !u.ManagesDept() && !u.SeesOrg() {
		return domain.ErrForbidden
	}
	if err := in.Validate(); err != nil {
		return err
	}
	by := u.Name
	if u.Position != "" {
		by += ", " + u.Position
	}
	return s.repo.AdmitAssignment(ctx, u, id, in, by)
}

func (s *Service) Batches(ctx context.Context, u domain.User) ([]domain.Batch, error) {
	return s.repo.Batches(ctx, u)
}

func (s *Service) BatchAssignments(ctx context.Context, u domain.User, batch string) ([]domain.Assignment, error) {
	return s.repo.BatchAssignments(ctx, u, batch)
}

// RecordKnowledge фиксирует результат проверки знаний и создаёт неизменяемый PDF одной строки журнала.
// После этого менять результат или содержимое записи нельзя — документ доступен только для подписания.
func (s *Service) RecordKnowledge(ctx context.Context, u domain.User, id int64, result string) error {
	if u.Role != domain.RoleInstructor && !u.ManagesDept() && !u.SeesOrg() {
		return domain.ErrForbidden
	}
	if !domain.ValidKnowledgeResult(result) {
		return domain.Invalid("Укажите результат проверки знаний")
	}
	a, err := s.repo.ScopedAssignment(ctx, u, id)
	if err != nil {
		return err
	}
	if a.KnowledgeResult != "" || a.RecordPDFPath != "" {
		return domain.Invalid("Результат проверки знаний уже зафиксирован")
	}
	t, err := s.repo.InductionTemplate(ctx, u.OrgID, a.TemplateID)
	if err != nil {
		return err
	}
	t.Body, t.FilePath = a.MaterialBody, a.MaterialFilePath
	materialSHA := a.MaterialSHA256
	if materialSHA == "" {
		materialHash := sha256.New()
		materialHash.Write([]byte(t.Body))
		if t.FilePath != "" {
			material, err := s.files.Read(u.OrgID, t.FilePath)
			if err != nil {
				return err
			}
			materialHash.Write(material)
		}
		materialSHA = fmt.Sprintf("%x", materialHash.Sum(nil))
	}
	org, err := s.dir.Org(ctx, u.OrgID)
	if err != nil {
		return err
	}
	instructor := a.Instructor
	if a.InstructorPos != "" {
		instructor += ", " + a.InstructorPos
	}
	pdfBytes, sum, err := s.protocol.Build(domain.ProtocolInput{
		Org: org, Template: t, Batch: a.Batch, AssignmentID: a.ID, AssignedAt: a.AssignedAt, DueAt: a.DueAt,
		Worker: a.UserName, BirthYear: a.BirthYear, Position: a.Position, Department: a.Department, Instructor: instructor,
		Location: a.WorkLocation, Reason: a.Reason, Basis: a.Basis, WorkType: a.WorkType, Docs: a.Docs,
		KnowledgeResult: result, MaterialSHA: materialSHA,
	})
	if err != nil {
		return err
	}
	pdfPath, err := s.files.Save(u.OrgID, domain.Upload{Name: fmt.Sprintf("induction-%d.pdf", a.ID), Size: int64(len(pdfBytes)), Content: bytes.NewReader(pdfBytes)}, "application/pdf")
	if err != nil {
		return err
	}
	return s.repo.SaveKnowledgeRecord(ctx, u, id, result, pdfPath, sum)
}

func (s *Service) KioskState(ctx context.Context, u domain.User, batch string) (domain.KioskState, error) {
	return s.repo.KioskState(ctx, u, batch)
}

func (s *Service) UserHistory(ctx context.Context, u domain.User, userID int64) ([]domain.Assignment, error) {
	if _, err := s.dir.UserName(ctx, u.OrgID, userID); err != nil {
		return nil, err
	}
	all, err := s.repo.UserAssignments(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	signed, err := s.repo.UserAssignments(ctx, userID, true)
	return append(all, signed...), err
}

// Journal — строки бумажной формы: вводный журнал или журнал инструктажа на рабочем месте.
func (s *Service) Journal(ctx context.Context, u domain.User, f domain.JournalFilter) ([]domain.Assignment, error) {
	kinds := []string{"intro"}
	if f.Workplace {
		kinds = domain.WorkplaceKinds
	}
	return s.repo.Journal(ctx, u, kinds, f)
}
