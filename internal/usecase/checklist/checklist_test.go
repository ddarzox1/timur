package checklist

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"qauipsiz/internal/domain"
)

var ctx = context.Background()

type memRepo struct {
	Repo
	tpl   domain.ChecklistTemplate
	run   *domain.Run
	saved *domain.ChecklistTemplate
}

func (m *memRepo) ChecklistTemplate(_ context.Context, org, id int64) (domain.ChecklistTemplate, error) {
	if org != 1 || id != m.tpl.ID {
		return domain.ChecklistTemplate{}, domain.ErrNotFound
	}
	return m.tpl, nil
}
func (m *memRepo) SaveRun(_ context.Context, _ int64, r domain.Run) error { m.run = &r; return nil }
func (m *memRepo) SaveChecklistTemplate(_ context.Context, _ int64, t domain.ChecklistTemplate) error {
	m.saved = &t
	return nil
}

type reporter struct{ calls []string }

func (r *reporter) Report(_ context.Context, _ int64, _, _ *int64, _, cat, risk, desc string, photos []string) (int64, error) {
	r.calls = append(r.calls, cat+"|"+risk+"|"+desc)
	return int64(100 + len(r.calls)), nil
}

type dir struct{}

func (dir) Location(_ context.Context, org, id int64) (domain.Location, error) {
	if org == 1 && id == 5 {
		return domain.Location{ID: 5}, nil
	}
	return domain.Location{}, domain.ErrNotFound
}

type files struct{}

func (files) Save(int64, domain.Upload, string) (string, error) { return "/files/1/p.jpg", nil }

var (
	u   = domain.User{ID: 10, OrgID: 1}
	sig = "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"+strings.Repeat("x", 300)))
)

func setup() (*Service, *memRepo, *reporter) {
	repo := &memRepo{tpl: domain.ChecklistTemplate{ID: 1, Title: "Осмотр лесов", Items: []domain.ChecklistItem{
		{Type: "section", Text: "Леса"},
		{Type: "question", Text: "Настил целый", HazardOnNo: true, Category: "height"},
		{Type: "question", Text: "Чисто", HazardOnNo: false, Category: "housekeeping"},
		{Type: "question", Text: "Каски", HazardOnNo: true, Category: "ppe"},
	}}}
	r := &reporter{}
	return New(repo, dir{}, files{}, r), repo, r
}

func TestSubmitCreatesHazards(t *testing.T) {
	s, repo, rep := setup()
	res, err := s.Submit(ctx, u, 1, map[int]AnswerInput{
		1: {Value: "no", Comment: " доска сломана ", Hazard: true, Photo: &domain.Upload{}},
		2: {Value: "no", Hazard: true},  // у пункта выключено «создавать опасность»
		3: {Value: "no", Hazard: false}, // сотрудник снял галочку
	}, sig)
	if err != nil {
		t.Fatal(err)
	}
	if res.Issues != 3 || res.HazardsCreated != 1 || len(rep.calls) != 1 {
		t.Fatalf("результат %+v, опасности %v", res, rep.calls)
	}
	if rep.calls[0] != "height|medium|Чек-лист «Осмотр лесов»: Настил целый. доска сломана" {
		t.Fatalf("опасность: %s", rep.calls[0])
	}
	r := repo.run
	if r.Total != 3 || r.Issues != 3 || len(r.Answers) != 4 || !r.Answers[0].Section || r.Answers[1].HazardID != 101 || r.Answers[1].Photo == "" {
		t.Fatalf("прохождение: %+v", r)
	}
}

func TestSubmitValidation(t *testing.T) {
	s, _, _ := setup()
	var ve domain.ValidationError
	if _, err := s.Submit(ctx, u, 1, map[int]AnswerInput{1: {Value: "yes"}, 2: {Value: "yes"}}, sig); !errors.As(err, &ve) || !strings.Contains(err.Error(), "Каски") {
		t.Fatalf("неотвеченный пункт: %v", err)
	}
	if _, err := s.Submit(ctx, u, 1, map[int]AnswerInput{1: {Value: "yes"}, 2: {Value: "yes"}, 3: {Value: "na"}}, "x"); !errors.As(err, &ve) {
		t.Fatal("без подписи")
	}
	if _, err := s.Submit(ctx, domain.User{OrgID: 2}, 1, nil, sig); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("чужой шаблон")
	}
}

func TestSaveTemplate(t *testing.T) {
	s, repo, _ := setup()
	err := s.SaveTemplate(ctx, u, domain.ChecklistTemplate{Title: " Склад ", Frequency: "hourly", Items: []domain.ChecklistItem{
		{Type: "section", Text: "Стеллажи", HazardOnNo: true, Category: "fire"},
		{Type: "question", Text: "  "},
		{Type: "question", Text: "Закреплены", Category: "bogus", HazardOnNo: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := repo.saved
	if got.Title != "Склад" || got.Frequency != "daily" || len(got.Items) != 2 || got.Items[0].HazardOnNo || got.Items[1].Category != "other" {
		t.Fatalf("нормализация: %+v", got)
	}
	var ve domain.ValidationError
	bad := int64(9)
	if err := s.SaveTemplate(ctx, u, domain.ChecklistTemplate{Title: "X", LocationID: &bad, Items: got.Items}); !errors.As(err, &ve) {
		t.Fatal("чужой объект")
	}
	if err := s.SaveTemplate(ctx, u, domain.ChecklistTemplate{Title: "X"}); !errors.As(err, &ve) {
		t.Fatal("без пунктов")
	}
}
