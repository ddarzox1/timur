// Package hazard — сообщения об опасностях: создание, назначение, устранение, история.
package hazard

import (
	"context"
	"time"

	"qauipsiz/internal/domain"
)

type Repo interface {
	CreateHazard(ctx context.Context, h domain.Hazard, photos []string) (int64, error)
	Hazard(ctx context.Context, orgID, id int64) (domain.Hazard, error)
	Hazards(ctx context.Context, orgID int64, f domain.HazardFilter) ([]domain.Hazard, error)
	UserHazards(ctx context.Context, orgID, userID int64, closed bool) ([]domain.Hazard, error)
	HazardPhotos(ctx context.Context, hazardID int64) ([]domain.Photo, error)
	HazardEvents(ctx context.Context, hazardID int64) ([]domain.Event, error)
	// UpdateHazard сохраняет поля и события в одной транзакции.
	UpdateHazard(ctx context.Context, h domain.Hazard, byUser int64, events []string) error
	FixHazard(ctx context.Context, orgID, hazardID, byUser int64, comment, photo string) error
	AddHazardEvent(ctx context.Context, hazardID int64, userID *int64, text string) error
	AssignHazards(ctx context.Context, orgID int64, ids []int64, responsibleID int64) ([]int64, error)
}

// Directory — справочные данные организации.
type Directory interface {
	Location(ctx context.Context, orgID, id int64) (domain.Location, error)
	UserName(ctx context.Context, orgID, id int64) (string, error)
}

type Storage interface {
	Save(orgID int64, f domain.Upload, onlyType string) (string, error)
}

type Service struct {
	repo  Repo
	dir   Directory
	files Storage
}

func New(repo Repo, dir Directory, files Storage) *Service {
	return &Service{repo: repo, dir: dir, files: files}
}

const MaxPhotos = 5

type CreateInput struct {
	LocationID  *int64
	Place       string
	Category    string
	Risk        string
	Description string
	Anonymous   bool
	Photos      []domain.Upload
}

// Create — сотрудник сообщает об опасности с телефона.
func (s *Service) Create(ctx context.Context, u domain.User, in CreateInput) (int64, error) {
	in.Description = domain.Clip(in.Description, domain.MaxText)
	if _, ok := domain.CategoryByKey(in.Category); !ok || domain.RiskLabels[in.Risk] == "" || in.Description == "" {
		return 0, domain.Invalid("Выберите категорию, уровень риска и опишите опасность")
	}
	if len(in.Photos) > MaxPhotos {
		return 0, domain.Invalid("Не больше 5 файлов")
	}
	var paths []string
	for _, f := range in.Photos {
		p, err := s.files.Save(u.OrgID, f, "")
		if err != nil {
			return 0, err
		}
		paths = append(paths, p)
	}
	author := &u.ID
	if in.Anonymous {
		author = nil
	}
	return s.Report(ctx, u.OrgID, author, in.LocationID, domain.Clip(in.Place, domain.MaxTitle), in.Category, in.Risk, in.Description, paths)
}

// Report регистрирует опасность с уже сохранёнными фото (форма сотрудника, чек-листы).
// Ответственный по умолчанию — ответственный за объект, срок — по уровню риска.
func (s *Service) Report(ctx context.Context, orgID int64, author, locationID *int64, place, cat, risk, desc string, photos []string) (int64, error) {
	h := domain.Hazard{OrgID: orgID, AuthorID: author, Place: place, Category: cat, Risk: risk, Description: desc, Status: "open"}
	if locationID != nil {
		loc, err := s.dir.Location(ctx, orgID, *locationID)
		switch {
		case err == nil:
			h.LocationID, h.ResponsibleID = &loc.ID, loc.ResponsibleID
		case err != domain.ErrNotFound:
			return 0, err
		}
	}
	due := domain.Today().AddDate(0, 0, domain.RiskDays[risk])
	h.DueAt = &due
	return s.repo.CreateHazard(ctx, h, photos)
}

// Details — опасность с фото и историей.
type Details struct {
	Hazard domain.Hazard
	Photos []domain.Photo
	Events []domain.Event
}

func (s *Service) Get(ctx context.Context, u domain.User, id int64) (Details, error) {
	h, err := s.repo.Hazard(ctx, u.OrgID, id)
	if err != nil {
		return Details{}, err
	}
	if !h.VisibleTo(u) {
		return Details{}, domain.ErrNotFound
	}
	d := Details{Hazard: h}
	if d.Photos, err = s.repo.HazardPhotos(ctx, id); err != nil {
		return d, err
	}
	d.Events, err = s.repo.HazardEvents(ctx, id)
	return d, err
}

func (s *Service) List(ctx context.Context, u domain.User, f domain.HazardFilter) ([]domain.Hazard, error) {
	return s.repo.Hazards(ctx, u.OrgID, f)
}

// Mine — опасности, которые сотрудник сообщил или которые ему назначены.
func (s *Service) Mine(ctx context.Context, u domain.User, closed bool) ([]domain.Hazard, error) {
	return s.repo.UserHazards(ctx, u.OrgID, u.ID, closed)
}

// Start — ответственный берёт опасность в работу.
func (s *Service) Start(ctx context.Context, u domain.User, id int64) error {
	h, err := s.repo.Hazard(ctx, u.OrgID, id)
	if err != nil {
		return err
	}
	if !h.IsResponsible(u) {
		return domain.ErrNotFound
	}
	if h.Status != "open" {
		return nil
	}
	h.Status = "in_progress"
	return s.repo.UpdateHazard(ctx, h, u.ID, []string{"Статус: В работе"})
}

// Fix — ответственный отмечает устранение: фото «после» и комментарий обязательны.
func (s *Service) Fix(ctx context.Context, u domain.User, id int64, comment string, photo *domain.Upload) error {
	comment = domain.Clip(comment, domain.MaxText)
	if comment == "" || photo == nil {
		return domain.Invalid("Нужны фото после устранения и комментарий")
	}
	h, err := s.repo.Hazard(ctx, u.OrgID, id)
	if err != nil {
		return err
	}
	if !h.IsResponsible(u) || !h.Active() {
		return domain.ErrNotFound
	}
	p, err := s.files.Save(u.OrgID, *photo, "image/")
	if err != nil {
		return err
	}
	return s.repo.FixHazard(ctx, u.OrgID, id, u.ID, comment, p)
}

func (s *Service) Comment(ctx context.Context, u domain.User, id int64, text string) error {
	text = domain.Clip(text, domain.MaxText)
	if text == "" {
		return domain.Invalid("Пустой комментарий")
	}
	h, err := s.repo.Hazard(ctx, u.OrgID, id)
	if err != nil {
		return err
	}
	if !h.VisibleTo(u) {
		return domain.ErrNotFound
	}
	return s.repo.AddHazardEvent(ctx, id, &u.ID, "💬 "+text)
}

type UpdateInput struct {
	ResponsibleID *int64
	DueAt         *time.Time
	Risk          string
	Status        string
	Action        string
}

// Update — инженер ОТ меняет ответственного, срок, риск, статус. Каждое изменение пишется в историю.
func (s *Service) Update(ctx context.Context, u domain.User, id int64, in UpdateInput) error {
	if domain.RiskLabels[in.Risk] == "" || domain.StatusLabels[in.Status] == "" {
		return domain.Invalid("Неверный риск или статус")
	}
	h, err := s.repo.Hazard(ctx, u.OrgID, id)
	if err != nil {
		return err
	}
	in.Action = domain.Clip(in.Action, domain.MaxText)
	var events []string
	if !domain.SameID(h.ResponsibleID, in.ResponsibleID) {
		name := "—"
		if in.ResponsibleID != nil {
			if name, err = s.dir.UserName(ctx, u.OrgID, *in.ResponsibleID); err != nil {
				return domain.Invalid("Неизвестный сотрудник")
			}
		}
		events = append(events, "Назначен ответственный: "+name)
	}
	if !sameDate(h.DueAt, in.DueAt) {
		events = append(events, "Срок: "+fmtDate(in.DueAt))
	}
	if h.Risk != in.Risk {
		events = append(events, "Риск: "+domain.RiskLabels[in.Risk])
	}
	if h.Status != in.Status {
		events = append(events, "Статус: "+domain.StatusLabels[in.Status])
	}
	if h.Action != in.Action && in.Action != "" {
		events = append(events, "Корректирующее действие: "+in.Action)
	}
	h.ResponsibleID, h.DueAt, h.Risk, h.Action = in.ResponsibleID, in.DueAt, in.Risk, in.Action
	switch {
	case in.Status == "fixed" && h.FixedAt == nil:
		now := domain.Now()
		h.FixedAt = &now
	case in.Status != "fixed":
		h.FixedAt = nil
	}
	h.Status = in.Status
	return s.repo.UpdateHazard(ctx, h, u.ID, events)
}

// AssignMany — массовое назначение ответственного из реестра.
func (s *Service) AssignMany(ctx context.Context, u domain.User, ids []int64, responsibleID *int64) error {
	if responsibleID == nil {
		return domain.Invalid("Выберите ответственного")
	}
	name, err := s.dir.UserName(ctx, u.OrgID, *responsibleID)
	if err != nil {
		return domain.Invalid("Выберите ответственного")
	}
	changed, err := s.repo.AssignHazards(ctx, u.OrgID, ids, *responsibleID)
	if err != nil {
		return err
	}
	for _, id := range changed {
		if err := s.repo.AddHazardEvent(ctx, id, &u.ID, "Назначен ответственный: "+name); err != nil {
			return err
		}
	}
	return nil
}

func sameDate(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Format(time.DateOnly) == b.Format(time.DateOnly))
}

func fmtDate(d *time.Time) string {
	if d == nil {
		return "—"
	}
	return d.Format("02.01.2006")
}
