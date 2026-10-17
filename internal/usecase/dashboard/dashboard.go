// Package dashboard — сводка для инженера ОТ и главная страница сотрудника.
package dashboard

import (
	"context"

	"qauipsiz/internal/domain"
)

type Counts struct {
	OpenHazards, OverdueHazards, Assigned, Signed, RunsWeek int
}

type HazardCounts struct{ Open, Fixed, AssignedToMe int }

type Repo interface {
	Org(ctx context.Context, id int64) (domain.Org, error)
	DashboardCounts(ctx context.Context, orgID int64, periodDays int) (Counts, error)
	HazardsByWeek(ctx context.Context, orgID int64) ([]domain.Bar, error)
	HazardsByCategory(ctx context.Context, orgID int64, periodDays int) ([]domain.Bar, error)
	SignaturesByDepartment(ctx context.Context, orgID int64, periodDays int) ([]domain.Bar, error)
	OverdueHazards(ctx context.Context, orgID int64, limit int) ([]domain.Hazard, error)
	OverdueAssignments(ctx context.Context, orgID int64, limit int) ([]domain.Assignment, error)
	// ExpiringRepeats — повторные инструктажи, истекающие в ближайшие days дней, без нового назначения.
	ExpiringRepeats(ctx context.Context, orgID int64, days int) ([]domain.Expiring, error)
	UserHazardCounts(ctx context.Context, orgID, userID int64) (HazardCounts, error)
	UserAssignments(ctx context.Context, userID int64, signed bool) ([]domain.Assignment, error)
	UserChecklists(ctx context.Context, orgID, userID int64, locationID *int64) ([]domain.ChecklistTemplate, error)
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
	Org               domain.Org
	DaysWithoutInjury int
	Counts
	Weeks, Categories, Departments []domain.Bar
	MaxWeek, MaxCategory           int
	OverdueHazards                 []domain.Hazard
	OverdueAssignments             []domain.Assignment
	Expiring                       []domain.Expiring
	Period                         int
}

func (s *Service) Stats(ctx context.Context, u domain.User, periodDays int) (Stats, error) {
	st := Stats{Period: NormalizePeriod(periodDays), MaxWeek: 1, MaxCategory: 1}
	var err error
	if st.Org, err = s.repo.Org(ctx, u.OrgID); err != nil {
		return st, err
	}
	st.DaysWithoutInjury = st.Org.DaysWithoutInjury(domain.Today())
	if st.Counts, err = s.repo.DashboardCounts(ctx, u.OrgID, st.Period); err != nil {
		return st, err
	}
	if st.Weeks, err = s.repo.HazardsByWeek(ctx, u.OrgID); err != nil {
		return st, err
	}
	if st.Categories, err = s.repo.HazardsByCategory(ctx, u.OrgID, st.Period); err != nil {
		return st, err
	}
	if st.Departments, err = s.repo.SignaturesByDepartment(ctx, u.OrgID, st.Period); err != nil {
		return st, err
	}
	if st.OverdueHazards, err = s.repo.OverdueHazards(ctx, u.OrgID, 10); err != nil {
		return st, err
	}
	if st.OverdueAssignments, err = s.repo.OverdueAssignments(ctx, u.OrgID, 10); err != nil {
		return st, err
	}
	if st.Expiring, err = s.repo.ExpiringRepeats(ctx, u.OrgID, st.Org.RepeatWarnDays); err != nil {
		return st, err
	}
	for _, b := range st.Weeks {
		st.MaxWeek = max(st.MaxWeek, b.A, b.B)
	}
	for _, b := range st.Categories {
		st.MaxCategory = max(st.MaxCategory, b.A)
	}
	return st, nil
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
