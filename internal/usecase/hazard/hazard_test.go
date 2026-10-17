package hazard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

var ctx = context.Background()

// memRepo хранит опасности в памяти. Методы, которых тест не ожидает, паникуют через встроенный nil-интерфейс.
type memRepo struct {
	Repo
	hazards map[int64]domain.Hazard
	photos  map[int64][]string
	events  map[int64][]string
}

func newRepo() *memRepo {
	return &memRepo{hazards: map[int64]domain.Hazard{}, photos: map[int64][]string{}, events: map[int64][]string{}}
}

func (m *memRepo) CreateHazard(_ context.Context, h domain.Hazard, photos []string) (int64, error) {
	h.ID = int64(len(m.hazards) + 1)
	m.hazards[h.ID] = h
	m.photos[h.ID] = photos
	m.events[h.ID] = []string{"Создано"}
	return h.ID, nil
}
func (m *memRepo) Hazard(_ context.Context, org, id int64) (domain.Hazard, error) {
	h, ok := m.hazards[id]
	if !ok || h.OrgID != org {
		return h, domain.ErrNotFound
	}
	return h, nil
}
func (m *memRepo) HazardPhotos(context.Context, int64) ([]domain.Photo, error) { return nil, nil }
func (m *memRepo) HazardEvents(context.Context, int64) ([]domain.Event, error) { return nil, nil }
func (m *memRepo) UpdateHazard(_ context.Context, h domain.Hazard, _ int64, ev []string) error {
	m.hazards[h.ID] = h
	m.events[h.ID] = append(m.events[h.ID], ev...)
	return nil
}
func (m *memRepo) FixHazard(_ context.Context, _, id, _ int64, comment, photo string) error {
	h := m.hazards[id]
	h.Status, h.Action = "fixed", comment
	m.hazards[id] = h
	m.photos[id] = append(m.photos[id], photo)
	return nil
}
func (m *memRepo) AddHazardEvent(_ context.Context, id int64, _ *int64, text string) error {
	m.events[id] = append(m.events[id], text)
	return nil
}
func (m *memRepo) AssignHazards(_ context.Context, org int64, ids []int64, resp int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if h, ok := m.hazards[id]; ok && h.OrgID == org {
			h.ResponsibleID = &resp
			m.hazards[id] = h
			out = append(out, id)
		}
	}
	return out, nil
}

type dir struct{}

var foreman = int64(20)

func (dir) Location(_ context.Context, org, id int64) (domain.Location, error) {
	if org == 1 && id == 5 {
		return domain.Location{ID: 5, ResponsibleID: &foreman}, nil
	}
	return domain.Location{}, domain.ErrNotFound
}
func (dir) UserName(_ context.Context, org, id int64) (string, error) {
	if org == 1 && (id == 20 || id == 30) {
		return "Сотрудник", nil
	}
	return "", domain.ErrNotFound
}

type files struct{ saved int }

func (f *files) Save(int64, domain.Upload, string) (string, error) {
	f.saved++
	return "/files/1/x.jpg", nil
}

var (
	worker  = domain.User{ID: 10, OrgID: 1, Role: domain.RoleWorker}
	resp    = domain.User{ID: 20, OrgID: 1, Role: domain.RoleWorker}
	other   = domain.User{ID: 30, OrgID: 1, Role: domain.RoleWorker}
	hse     = domain.User{ID: 40, OrgID: 1, Role: domain.RoleHSE}
	outside = domain.User{ID: 50, OrgID: 2, Role: domain.RoleAdmin}
)

func setup(t *testing.T) (*Service, *memRepo, *files) {
	old := domain.Now
	domain.Now = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, domain.KZ) }
	t.Cleanup(func() { domain.Now = old })
	repo, fs := newRepo(), &files{}
	return New(repo, dir{}, fs), repo, fs
}

func create(t *testing.T, s *Service, loc *int64, risk string) int64 {
	id, err := s.Create(ctx, worker, CreateInput{LocationID: loc, Category: "electric", Risk: risk, Description: " Оголённый кабель ",
		Photos: []domain.Upload{{Name: "a.jpg"}}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateDefaults(t *testing.T) {
	s, repo, fs := setup(t)
	loc := int64(5)
	id := create(t, s, &loc, "high")
	h := repo.hazards[id]
	if h.ResponsibleID == nil || *h.ResponsibleID != foreman {
		t.Fatal("ответственный должен браться из объекта")
	}
	if h.DueAt.Format(time.DateOnly) != "2026-10-17" {
		t.Fatalf("высокий риск — срок 1 день, получили %s", h.DueAt.Format(time.DateOnly))
	}
	if h.Description != "Оголённый кабель" || *h.AuthorID != worker.ID || fs.saved != 1 {
		t.Fatalf("поля: %+v, сохранено файлов %d", h, fs.saved)
	}

	foreign := int64(999) // объект другой организации игнорируется
	h = repo.hazards[create(t, s, &foreign, "low")]
	if h.LocationID != nil || h.ResponsibleID != nil || h.DueAt.Format(time.DateOnly) != "2026-10-23" {
		t.Fatalf("чужой объект: %+v", h)
	}
}

func TestCreateValidation(t *testing.T) {
	s, _, _ := setup(t)
	for _, in := range []CreateInput{
		{Category: "nope", Risk: "low", Description: "x"},
		{Category: "fire", Risk: "extreme", Description: "x"},
		{Category: "fire", Risk: "low", Description: "  "},
		{Category: "fire", Risk: "low", Description: "x", Photos: make([]domain.Upload, 6)},
	} {
		var ve domain.ValidationError
		if _, err := s.Create(ctx, worker, in); !errors.As(err, &ve) {
			t.Errorf("%+v: ожидалась ошибка валидации, получили %v", in, err)
		}
	}
	id, _ := s.Create(ctx, worker, CreateInput{Category: "fire", Risk: "low", Description: "x", Anonymous: true})
	if s.repo.(*memRepo).hazards[id].AuthorID != nil {
		t.Fatal("анонимная опасность не должна хранить автора")
	}
}

func TestAccess(t *testing.T) {
	s, _, _ := setup(t)
	loc := int64(5)
	id := create(t, s, &loc, "medium")
	for _, c := range []struct {
		u  domain.User
		ok bool
	}{{worker, true}, {resp, true}, {hse, true}, {other, false}, {outside, false}} {
		_, err := s.Get(ctx, c.u, id)
		if (err == nil) != c.ok {
			t.Errorf("Get пользователем %d: err=%v", c.u.ID, err)
		}
	}
	if err := s.Comment(ctx, other, id, "привет"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("посторонний не может комментировать")
	}
}

func TestStartAndFix(t *testing.T) {
	s, repo, _ := setup(t)
	loc := int64(5)
	id := create(t, s, &loc, "medium")
	photo := &domain.Upload{Name: "after.jpg"}

	if err := s.Start(ctx, worker, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("взять в работу может только ответственный")
	}
	if err := s.Start(ctx, resp, id); err != nil || repo.hazards[id].Status != "in_progress" {
		t.Fatalf("Start: %v %s", err, repo.hazards[id].Status)
	}
	var ve domain.ValidationError
	if err := s.Fix(ctx, resp, id, "  ", photo); !errors.As(err, &ve) {
		t.Fatal("без комментария нельзя")
	}
	if err := s.Fix(ctx, resp, id, "Заизолировали", nil); !errors.As(err, &ve) {
		t.Fatal("без фото нельзя")
	}
	if err := s.Fix(ctx, worker, id, "x", photo); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("устранить может только ответственный")
	}
	if err := s.Fix(ctx, resp, id, "Заизолировали", photo); err != nil || repo.hazards[id].Status != "fixed" {
		t.Fatalf("Fix: %v", err)
	}
	if err := s.Fix(ctx, resp, id, "ещё раз", photo); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("устранённую нельзя устранить повторно")
	}
}

func TestUpdateWritesOnlyChanges(t *testing.T) {
	s, repo, _ := setup(t)
	loc := int64(5)
	id := create(t, s, &loc, "medium")
	h := repo.hazards[id]
	err := s.Update(ctx, hse, id, UpdateInput{ResponsibleID: h.ResponsibleID, DueAt: h.DueAt, Risk: "high", Status: "fixed", Action: "Ограждение"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(repo.events[id], " | ")
	if got != "Создано | Риск: Высокий | Статус: Устранено | Корректирующее действие: Ограждение" {
		t.Fatalf("события: %s", got)
	}
	if repo.hazards[id].FixedAt == nil {
		t.Fatal("fixed_at должен проставиться")
	}
	var ve domain.ValidationError
	stranger := int64(999)
	if err := s.Update(ctx, hse, id, UpdateInput{ResponsibleID: &stranger, Risk: "high", Status: "open"}); !errors.As(err, &ve) {
		t.Fatal("ответственный из чужой организации должен отклоняться")
	}
	if err := s.Update(ctx, hse, id, UpdateInput{Risk: "high", Status: "open"}); err != nil || repo.hazards[id].FixedAt != nil {
		t.Fatal("при возврате в работу fixed_at сбрасывается")
	}
}

func TestAssignMany(t *testing.T) {
	s, repo, _ := setup(t)
	a, b := create(t, s, nil, "low"), create(t, s, nil, "low")
	r := int64(30)
	if err := s.AssignMany(ctx, hse, []int64{a, b, 777}, &r); err != nil {
		t.Fatal(err)
	}
	if *repo.hazards[a].ResponsibleID != 30 || len(repo.events[b]) != 2 {
		t.Fatal("назначение и событие")
	}
	if err := s.AssignMany(ctx, hse, []int64{a}, nil); err == nil {
		t.Fatal("без ответственного — ошибка")
	}
}
