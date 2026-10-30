package dashboard

import (
	"context"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

type memRepo struct {
	Repo
	warn int
}

func (m *memRepo) Org(context.Context, int64) (domain.Org, error) {
	return domain.Org{RepeatWarnDays: 21, CreatedAt: time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)}, nil
}
func (m *memRepo) InstructionKPI(context.Context, domain.User) (KPI, error) {
	return KPI{Total: 10, Previous: 8, Completed: 6, WaitingWorkers: 3, WaitingInstructor: 1}, nil
}
func (m *memRepo) InstructionDynamics(context.Context, domain.User) ([]domain.Bar, error) {
	return []domain.Bar{{Label: "Sep", A: 2, B: 1}, {Label: "Oct", A: 7, B: 4}}, nil
}
func (m *memRepo) InstructionDepartments(context.Context, domain.User) ([]domain.Bar, error) {
	return []domain.Bar{{Label: "Цех", A: 5, B: 4}}, nil
}
func (m *memRepo) RecentSessions(context.Context, domain.User, int) ([]Session, error) {
	return []Session{{Batch: "b", Title: "Высота", Total: 2, Signed: 2}}, nil
}
func (m *memRepo) UpcomingSessions(context.Context, domain.User, int) ([]Session, error) {
	return []Session{{Batch: "c", Total: 2, Signed: 1}}, nil
}
func (m *memRepo) UserActivity(context.Context, domain.User, int) ([]Activity, error) {
	return []Activity{{ID: 1, Name: "Айгерим"}}, nil
}
func (m *memRepo) OverdueAssignments(context.Context, domain.User, int) ([]domain.Assignment, error) {
	return []domain.Assignment{{Batch: "late", UserName: "Ерлан", Title: "Высота"}}, nil
}
func (m *memRepo) ExpiringRepeats(_ context.Context, _ domain.User, days int) ([]domain.Expiring, error) {
	m.warn = days
	return []domain.Expiring{{UserName: "Данияр", Title: "Повторный", Template: 3}}, nil
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
	if repo.warn != 21 {
		t.Fatal("порог предупреждения берётся из настроек организации")
	}
	if st.KPI.ChangePct() != 25 || st.KPI.Completed != 6 || len(st.Dynamics) != 2 || len(st.Recent) != 1 || len(st.Notifications) != 3 || st.LineTotal == "" || st.LineCompleted == "" {
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
