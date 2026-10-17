// Package checklist — чек-листы осмотров: шаблоны, прохождение, результаты.
package checklist

import (
	"context"
	"fmt"

	"qauipsiz/internal/domain"
)

type Repo interface {
	ChecklistTemplates(ctx context.Context, orgID int64) ([]domain.ChecklistTemplate, error)
	// UserChecklists — шаблоны для объекта сотрудника (и общие), с отметкой «пройден сегодня».
	UserChecklists(ctx context.Context, orgID, userID int64, locationID *int64) ([]domain.ChecklistTemplate, error)
	ChecklistTemplate(ctx context.Context, orgID, id int64) (domain.ChecklistTemplate, error)
	SaveChecklistTemplate(ctx context.Context, orgID int64, t domain.ChecklistTemplate) error
	SaveRun(ctx context.Context, orgID int64, r domain.Run) error
	Runs(ctx context.Context, orgID int64) ([]domain.Run, error)
	Run(ctx context.Context, orgID, id int64) (domain.Run, error)
}

type Directory interface {
	Location(ctx context.Context, orgID, id int64) (domain.Location, error)
}

type Storage interface {
	Save(orgID int64, f domain.Upload, onlyType string) (string, error)
}

// HazardReporter — регистрация опасности по пункту «Нет» (реализует usecase hazard).
type HazardReporter interface {
	Report(ctx context.Context, orgID int64, author, locationID *int64, place, cat, risk, desc string, photos []string) (int64, error)
}

type Service struct {
	repo    Repo
	dir     Directory
	files   Storage
	hazards HazardReporter
}

func New(repo Repo, dir Directory, files Storage, hazards HazardReporter) *Service {
	return &Service{repo: repo, dir: dir, files: files, hazards: hazards}
}

func (s *Service) Templates(ctx context.Context, u domain.User) ([]domain.ChecklistTemplate, error) {
	return s.repo.ChecklistTemplates(ctx, u.OrgID)
}

func (s *Service) ForUser(ctx context.Context, u domain.User) ([]domain.ChecklistTemplate, error) {
	return s.repo.UserChecklists(ctx, u.OrgID, u.ID, u.LocationID)
}

func (s *Service) Template(ctx context.Context, u domain.User, id int64) (domain.ChecklistTemplate, error) {
	return s.repo.ChecklistTemplate(ctx, u.OrgID, id)
}

// SaveTemplate — пустые пункты отбрасываются; у разделов нет флага опасности.
func (s *Service) SaveTemplate(ctx context.Context, u domain.User, t domain.ChecklistTemplate) error {
	t.Title = domain.Clip(t.Title, domain.MaxTitle)
	if t.Title == "" {
		return domain.Invalid("Укажите название чек-листа")
	}
	var items []domain.ChecklistItem
	for _, it := range t.Items {
		it.Text = domain.Clip(it.Text, domain.MaxTitle*2)
		if it.Text == "" {
			continue
		}
		if it.Type == "section" {
			it = domain.ChecklistItem{Type: "section", Text: it.Text}
		} else {
			it.Type = "question"
			if _, ok := domain.CategoryByKey(it.Category); !ok {
				it.Category = "other"
			}
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return domain.Invalid("Добавьте хотя бы один пункт")
	}
	t.Items = items
	if domain.FreqLabels[t.Frequency] == "" {
		t.Frequency = "daily"
	}
	if t.LocationID != nil {
		if _, err := s.dir.Location(ctx, u.OrgID, *t.LocationID); err != nil {
			return domain.Invalid("Неизвестный объект")
		}
	}
	return s.repo.SaveChecklistTemplate(ctx, u.OrgID, t)
}

// AnswerInput — ответ на пункт с индексом в Items шаблона.
type AnswerInput struct {
	Value   string // yes | no | na
	Comment string
	Photo   *domain.Upload
	Hazard  bool // создать опасность при «Нет»
}

type Result struct{ Issues, HazardsCreated int }

// Submit сохраняет прохождение. Каждое «Нет» с флагом сразу становится опасностью со ссылкой из ответа.
// ponytail: опасности и прохождение пишутся не в одной транзакции — при сбое останутся опасности без прохождения.
func (s *Service) Submit(ctx context.Context, u domain.User, id int64, answers map[int]AnswerInput, signature string) (Result, error) {
	t, err := s.repo.ChecklistTemplate(ctx, u.OrgID, id)
	if err != nil {
		return Result{}, err
	}
	if !domain.ValidSignature(signature) {
		return Result{}, domain.Invalid("Поставьте подпись проверяющего")
	}
	for i, it := range t.Items {
		if v := answers[i].Value; it.Type != "section" && v != "yes" && v != "no" && v != "na" {
			return Result{}, domain.Invalid("Ответьте на все пункты: «" + it.Text + "»")
		}
	}
	loc := t.LocationID
	if loc == nil {
		loc = u.LocationID
	}
	run := domain.Run{TemplateID: t.ID, UserID: &u.ID, LocationID: loc, Signature: signature}
	var res Result
	for i, it := range t.Items {
		if it.Type == "section" {
			run.Answers = append(run.Answers, domain.Answer{Text: it.Text, Section: true})
			continue
		}
		in := answers[i]
		a := domain.Answer{Text: it.Text, Value: in.Value, Comment: domain.Clip(in.Comment, domain.MaxText)}
		run.Total++
		if in.Photo != nil {
			if a.Photo, err = s.files.Save(u.OrgID, *in.Photo, "image/"); err != nil {
				return Result{}, err
			}
		}
		if a.Value == "no" {
			res.Issues++
			if in.Hazard && it.HazardOnNo {
				desc := fmt.Sprintf("Чек-лист «%s»: %s", t.Title, it.Text)
				if a.Comment != "" {
					desc += ". " + a.Comment
				}
				var photos []string
				if a.Photo != "" {
					photos = []string{a.Photo}
				}
				if a.HazardID, err = s.hazards.Report(ctx, u.OrgID, &u.ID, loc, "", it.Category, "medium", desc, photos); err != nil {
					return Result{}, err
				}
				res.HazardsCreated++
			}
		}
		run.Answers = append(run.Answers, a)
	}
	run.Issues = res.Issues
	return res, s.repo.SaveRun(ctx, u.OrgID, run)
}

func (s *Service) Runs(ctx context.Context, u domain.User) ([]domain.Run, error) {
	return s.repo.Runs(ctx, u.OrgID)
}

func (s *Service) Run(ctx context.Context, u domain.User, id int64) (domain.Run, error) {
	return s.repo.Run(ctx, u.OrgID, id)
}
