// Package dashboard — сводка для инженера ОТ и главная страница сотрудника.
package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"qauipsiz/internal/domain"
)

type Counts struct {
	OpenHazards, OverdueHazards, Assigned, Signed, RunsWeek int
}

type KPI struct {
	Total, Previous, Completed, WaitingWorkers, WaitingInstructor, Overdue int
}

func (k KPI) ChangePct() int {
	if k.Previous == 0 {
		if k.Total > 0 {
			return 100
		}
		return 0
	}
	return (k.Total - k.Previous) * 100 / k.Previous
}

type Session struct {
	Batch              string     `db:"batch"`
	Title              string     `db:"title"`
	Kind               string     `db:"kind"`
	AssignedAt         time.Time  `db:"assigned_at"`
	DueAt              time.Time  `db:"due_at"`
	Total              int        `db:"total"`
	Signed             int        `db:"signed"`
	InstructorSignedAt *time.Time `db:"instructor_signed_at"`
	Department         string     `db:"department"`
}

func (s Session) Status() string {
	if s.InstructorSignedAt != nil {
		return "completed"
	}
	if s.Signed < s.Total {
		return "workers"
	}
	return "instructor"
}

type Activity struct {
	ID         int64      `db:"id"`
	Name       string     `db:"name"`
	Role       string     `db:"role"`
	Department string     `db:"department"`
	LastSeenAt *time.Time `db:"last_seen_at"`
}

type Notification struct{ Kind, Text, Href string }

type HazardCounts struct{ Open, Fixed, AssignedToMe int }

type Repo interface {
	Org(ctx context.Context, id int64) (domain.Org, error)
	DashboardCounts(ctx context.Context, viewer domain.User, periodDays int) (Counts, error)
	HazardsByWeek(ctx context.Context, viewer domain.User) ([]domain.Bar, error)
	HazardsByCategory(ctx context.Context, viewer domain.User, periodDays int) ([]domain.Bar, error)
	SignaturesByDepartment(ctx context.Context, viewer domain.User, periodDays int) ([]domain.Bar, error)
	OverdueHazards(ctx context.Context, viewer domain.User, limit int) ([]domain.Hazard, error)
	OverdueAssignments(ctx context.Context, viewer domain.User, limit int) ([]domain.Assignment, error)
	// ExpiringRepeats — повторные инструктажи, истекающие в ближайшие days дней, без нового назначения.
	ExpiringRepeats(ctx context.Context, viewer domain.User, days int) ([]domain.Expiring, error)
	UserHazardCounts(ctx context.Context, orgID, userID int64) (HazardCounts, error)
	UserAssignments(ctx context.Context, userID int64, signed bool) ([]domain.Assignment, error)
	UserChecklists(ctx context.Context, orgID, userID int64, locationID *int64) ([]domain.ChecklistTemplate, error)
	InstructionKPI(ctx context.Context, viewer domain.User) (KPI, error)
	InstructionDynamics(ctx context.Context, viewer domain.User) ([]domain.Bar, error)
	InstructionDepartments(ctx context.Context, viewer domain.User) ([]domain.Bar, error)
	RecentSessions(ctx context.Context, viewer domain.User, limit int) ([]Session, error)
	UpcomingSessions(ctx context.Context, viewer domain.User, limit int) ([]Session, error)
	UserActivity(ctx context.Context, viewer domain.User, limit int) ([]Activity, error)
}

type Service struct{ repo Repo }

func New(repo Repo) *Service { return &Service{repo: repo} }

// Periods — допустимые периоды графиков, в днях.
var Periods = []int{30, 90, 365}

func NormalizePeriod(days int) int {
	for _, p := range Periods {
		if p == days {
			return p
		}
	}
	return 90
}

type Stats struct {
	Org                      domain.Org
	KPI                      KPI
	Dynamics, Departments    []domain.Bar
	Recent, Upcoming         []Session
	Activity                 []Activity
	Notifications            []Notification
	LineTotal, LineCompleted string
}

func (s *Service) Stats(ctx context.Context, u domain.User, periodDays int) (Stats, error) {
	var st Stats
	var err error
	if st.Org, err = s.repo.Org(ctx, u.OrgID); err != nil {
		return st, err
	}
	if st.KPI, err = s.repo.InstructionKPI(ctx, u); err != nil {
		return st, err
	}
	if st.Dynamics, err = s.repo.InstructionDynamics(ctx, u); err != nil {
		return st, err
	}
	if st.Departments, err = s.repo.InstructionDepartments(ctx, u); err != nil {
		return st, err
	}
	if st.Recent, err = s.repo.RecentSessions(ctx, u, 6); err != nil {
		return st, err
	}
	if st.Upcoming, err = s.repo.UpcomingSessions(ctx, u, 6); err != nil {
		return st, err
	}
	if st.Activity, err = s.repo.UserActivity(ctx, u, 8); err != nil {
		return st, err
	}
	overdue, err := s.repo.OverdueAssignments(ctx, u, 5)
	if err != nil {
		return st, err
	}
	expiring, err := s.repo.ExpiringRepeats(ctx, u, st.Org.RepeatWarnDays)
	if err != nil {
		return st, err
	}
	for _, x := range overdue {
		st.Notifications = append(st.Notifications, Notification{"danger", fmt.Sprintf("%s просрочил(а) «%s»", x.UserName, x.Title), "/admin/assignments?batch=" + x.Batch})
	}
	for _, x := range expiring {
		st.Notifications = append(st.Notifications, Notification{"warn", fmt.Sprintf("У %s истекает повторный инструктаж «%s»", x.UserName, x.Title), fmt.Sprintf("/admin/assign?template=%d", x.Template)})
	}
	for _, x := range st.Recent {
		if x.Status() == "instructor" {
			st.Notifications = append(st.Notifications, Notification{"info", "Протокол «" + x.Title + "» ждёт подписи инструктирующего", "/admin/sessions/" + x.Batch + "/kiosk"})
		}
	}
	st.LineTotal, st.LineCompleted = linePoints(st.Dynamics)
	return st, nil
}

func linePoints(rows []domain.Bar) (string, string) {
	maxValue := 1
	for _, x := range rows {
		maxValue = max(maxValue, x.A, x.B)
	}
	total, done := make([]string, 0, len(rows)), make([]string, 0, len(rows))
	for i, x := range rows {
		px := 8
		if len(rows) > 1 {
			px += i * 84 / (len(rows) - 1)
		}
		total = append(total, fmt.Sprintf("%d,%d", px, 88-x.A*70/maxValue))
		done = append(done, fmt.Sprintf("%d,%d", px, 88-x.B*70/maxValue))
	}
	return strings.Join(total, " "), strings.Join(done, " ")
}

type Home struct {
	DaysWithoutInjury int
	Pending           []domain.Assignment
	Checklists        []domain.ChecklistTemplate
	Hazards           HazardCounts
}

func (s *Service) WorkerHome(ctx context.Context, u domain.User) (Home, error) {
	var h Home
	o, err := s.repo.Org(ctx, u.OrgID)
	if err != nil {
		return h, err
	}
	h.DaysWithoutInjury = o.DaysWithoutInjury(domain.Today())
	if h.Pending, err = s.repo.UserAssignments(ctx, u.ID, false); err != nil {
		return h, err
	}
	if h.Checklists, err = s.repo.UserChecklists(ctx, u.OrgID, u.ID, u.LocationID); err != nil {
		return h, err
	}
	h.Hazards, err = s.repo.UserHazardCounts(ctx, u.OrgID, u.ID)
	return h, err
}
