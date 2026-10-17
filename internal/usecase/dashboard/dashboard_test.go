package dashboard

import (
	"context"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

type memRepo struct {
	Repo
	period int
	warn   int
}

func (m *memRepo) Org(context.Context, int64) (domain.Org, error) {
	return domain.Org{RepeatWarnDays: 21, CreatedAt: time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)}, nil
}
func (m *memRepo) DashboardCounts(_ context.Context, _ int64, p int) (Counts, error) {
	m.period = p
	return Counts{OpenHazards: 3}, nil
}
func (m *memRepo) HazardsByWeek(context.Context, int64) ([]domain.Bar, error) {
	return []domain.Bar{{A: 2, B: 7}, {A: 4, B: 1}}, nil
}
func (m *memRepo) HazardsByCategory(context.Context, int64, int) ([]domain.Bar, error) {
	return []domain.Bar{{A: 5}}, nil
}
func (m *memRepo) SignaturesByDepartment(context.Context, int64, int) ([]domain.Bar, error) {
	return nil, nil
}
func (m *memRepo) OverdueHazards(context.Context, int64, int) ([]domain.Hazard, error) {
	return nil, nil
}
func (m *memRepo) OverdueAssignments(context.Context, int64, int) ([]domain.Assignment, error) {
	return nil, nil
}
func (m *memRepo) ExpiringRepeats(_ context.Context, _ int64, days int) ([]domain.Expiring, error) {
	m.warn = days
	return nil, nil
}

func TestStats(t *testing.T) {
	old := domain.Now
	domain.Now = func() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, domain.KZ) }
	defer func() { domain.Now = old }()

	repo := &memRepo{}
	st, err := New(repo).Stats(context.Background(), domain.User{OrgID: 1}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if repo.period != 90 || st.Period != 90 {
		t.Fatalf("неизвестный период → 90, получили %d", repo.period)
	}
	if repo.warn != 21 {
		t.Fatal("порог предупреждения берётся из настроек организации")
	}
	if st.MaxWeek != 7 || st.MaxCategory != 5 || st.DaysWithoutInjury != 10 || st.OpenHazards != 3 {
		t.Fatalf("%+v", st)
	}
	for _, p := range Periods {
		if NormalizePeriod(p) != p {
			t.Fatalf("период %d", p)
		}
	}
}

func (m *memRepo) UserAssignments(context.Context, int64, bool) ([]domain.Assignment, error) {
	return []domain.Assignment{{ID: 1}}, nil
}
func (m *memRepo) UserChecklists(_ context.Context, _, _ int64, loc *int64) ([]domain.ChecklistTemplate, error) {
	if loc == nil || *loc != 5 {
		return nil, nil
	}
	return []domain.ChecklistTemplate{{ID: 9}}, nil
}
func (m *memRepo) UserHazardCounts(context.Context, int64, int64) (HazardCounts, error) {
	return HazardCounts{Open: 2, AssignedToMe: 1}, nil
}

func TestWorkerHome(t *testing.T) {
	old := domain.Now
	domain.Now = func() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, domain.KZ) }
	defer func() { domain.Now = old }()
	loc := int64(5)
	h, err := New(&memRepo{}).WorkerHome(context.Background(), domain.User{ID: 3, OrgID: 1, LocationID: &loc})
	if err != nil {
		t.Fatal(err)
	}
	if h.DaysWithoutInjury != 10 || len(h.Pending) != 1 || len(h.Checklists) != 1 || h.Hazards.Open != 2 || h.Hazards.AssignedToMe != 1 {
		t.Fatalf("%+v", h)
	}
}
