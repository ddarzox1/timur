package postgres

import (
	"context"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/dashboard"
)

const dashboardSessionsCTE = `WITH scoped_sessions AS (
	SELECT a.batch, min(t.title) AS title, min(t.kind) AS kind, min(a.assigned_at) AS assigned_at,
		max(a.due_at)::timestamptz AS due_at, count(*)::int AS total, count(a.signed_at)::int AS signed,
		CASE WHEN count(ir.instructor_signed_at)=count(*) THEN max(ir.instructor_signed_at) END AS instructor_signed_at,
		string_agg(DISTINCT coalesce(d.name, 'Без подразделения'), ', ') AS department
	FROM induction_assignments a
	JOIN induction_templates t ON t.id=a.template_id
	JOIN users u ON u.id=a.user_id
	LEFT JOIN departments d ON d.id=u.department_id
	LEFT JOIN induction_records ir ON ir.assignment_id=a.id
	WHERE a.org_id=$1 AND ($2::bigint[] IS NULL OR u.department_id=ANY($2) OR ($3='instructor' AND a.instructor_id=$4))
	GROUP BY a.batch
) `

func (s *Store) InstructionKPI(ctx context.Context, viewer domain.User) (dashboard.KPI, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return dashboard.KPI{}, err
	}
	var k dashboard.KPI
	err = s.db.QueryRow(ctx, dashboardSessionsCTE+`SELECT
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now()))::int,
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now())-interval '1 month' AND assigned_at < date_trunc('month', now()))::int,
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now()) AND instructor_signed_at IS NOT NULL)::int,
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now()) AND signed < total)::int,
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now()) AND signed=total AND instructor_signed_at IS NULL)::int,
		count(*) FILTER (WHERE assigned_at >= date_trunc('month', now()) AND due_at < current_date AND instructor_signed_at IS NULL)::int
		FROM scoped_sessions`, viewer.OrgID, deps, viewer.Role, viewer.ID).
		Scan(&k.Total, &k.Previous, &k.Completed, &k.WaitingWorkers, &k.WaitingInstructor, &k.Overdue)
	return k, err
}

func (s *Store) InstructionDynamics(ctx context.Context, viewer domain.User) ([]domain.Bar, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Bar](ctx, s.db, dashboardSessionsCTE+`, months AS (
		SELECT generate_series(date_trunc('month', now())-interval '5 months', date_trunc('month', now()), interval '1 month') m
	) SELECT to_char(m.m, 'Mon') AS label, count(x.batch)::int AS a,
		count(x.batch) FILTER (WHERE x.instructor_signed_at IS NOT NULL)::int AS b
		FROM months m LEFT JOIN scoped_sessions x ON x.assigned_at >= m.m AND x.assigned_at < m.m+interval '1 month'
		GROUP BY m.m ORDER BY m.m`, viewer.OrgID, deps, viewer.Role, viewer.ID)
}

func (s *Store) InstructionDepartments(ctx context.Context, viewer domain.User) ([]domain.Bar, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Bar](ctx, s.db, `SELECT coalesce(d.name,'Без подразделения') AS label, count(*)::int AS a,
		count(a.signed_at)::int AS b FROM induction_assignments a JOIN users u ON u.id=a.user_id
		LEFT JOIN departments d ON d.id=u.department_id
		WHERE a.org_id=$1 AND a.assigned_at >= date_trunc('month',now())
		AND ($2::bigint[] IS NULL OR u.department_id=ANY($2) OR ($3='instructor' AND a.instructor_id=$4))
		GROUP BY d.name ORDER BY a DESC`, viewer.OrgID, deps, viewer.Role, viewer.ID)
}

func (s *Store) RecentSessions(ctx context.Context, viewer domain.User, limit int) ([]dashboard.Session, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[dashboard.Session](ctx, s.db, dashboardSessionsCTE+`SELECT * FROM scoped_sessions ORDER BY assigned_at DESC LIMIT $5`,
		viewer.OrgID, deps, viewer.Role, viewer.ID, limit)
}

func (s *Store) UpcomingSessions(ctx context.Context, viewer domain.User, limit int) ([]dashboard.Session, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[dashboard.Session](ctx, s.db, dashboardSessionsCTE+`SELECT * FROM scoped_sessions
		WHERE instructor_signed_at IS NULL AND due_at >= current_date ORDER BY due_at LIMIT $5`,
		viewer.OrgID, deps, viewer.Role, viewer.ID, limit)
}

func (s *Store) UserActivity(ctx context.Context, viewer domain.User, limit int) ([]dashboard.Activity, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[dashboard.Activity](ctx, s.db, `SELECT u.id, u.name, u.role, coalesce(d.name,'Без подразделения') AS department, u.last_seen_at
		FROM users u LEFT JOIN departments d ON d.id=u.department_id
		WHERE u.org_id=$1 AND u.role<>'visitor' AND ($2::bigint[] IS NULL OR u.department_id=ANY($2))
		ORDER BY u.last_seen_at DESC NULLS LAST, u.name LIMIT $3`, viewer.OrgID, deps, limit)
}

func (s *Store) DashboardCounts(ctx context.Context, viewer domain.User, days int) (dashboard.Counts, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return dashboard.Counts{}, err
	}
	var c dashboard.Counts
	err = s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM hazards h WHERE h.org_id = $1 AND h.status IN ('open', 'in_progress') AND ($3::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($3)))),
		(SELECT count(*) FROM hazards h WHERE h.org_id = $1 AND h.status IN ('open', 'in_progress') AND h.due_at < current_date AND ($3::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($3)))),
		(SELECT count(*) FROM induction_assignments x JOIN users xu ON xu.id = x.user_id WHERE x.org_id = $1 AND x.assigned_at > now() - make_interval(days => $2) AND ($3::bigint[] IS NULL OR xu.department_id = ANY($3) OR ($4 = 'instructor' AND x.instructor_id = $5))),
		(SELECT count(x.signed_at) FROM induction_assignments x JOIN users xu ON xu.id = x.user_id WHERE x.org_id = $1 AND x.assigned_at > now() - make_interval(days => $2) AND ($3::bigint[] IS NULL OR xu.department_id = ANY($3) OR ($4 = 'instructor' AND x.instructor_id = $5))),
		(SELECT count(*) FROM checklist_runs r LEFT JOIN users ru ON ru.id = r.user_id WHERE r.org_id = $1 AND r.created_at > now() - interval '7 days' AND ($3::bigint[] IS NULL OR ru.department_id = ANY($3)))`, viewer.OrgID, days, deps, viewer.Role, viewer.ID).
		Scan(&c.OpenHazards, &c.OverdueHazards, &c.Assigned, &c.Signed, &c.RunsWeek)
	return c, err
}

func (s *Store) HazardsByWeek(ctx context.Context, viewer domain.User) ([]domain.Bar, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Bar](ctx, s.db, `SELECT to_char(w, 'DD.MM') AS label,
		(SELECT count(*) FROM hazards h WHERE org_id = $1 AND created_at >= w AND created_at < w + interval '7 days' AND ($2::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($2)))) AS a,
		(SELECT count(*) FROM hazards h WHERE org_id = $1 AND fixed_at >= w AND fixed_at < w + interval '7 days' AND ($2::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($2)))) AS b
		FROM generate_series(date_trunc('week', now()) - interval '7 weeks', date_trunc('week', now()), interval '1 week') w`, viewer.OrgID, deps)
}

func (s *Store) HazardsByCategory(ctx context.Context, viewer domain.User, days int) ([]domain.Bar, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Bar](ctx, s.db, `SELECT category AS label, count(*) AS a, count(*) FILTER (WHERE status IN ('open', 'in_progress')) AS b
		FROM hazards h WHERE org_id = $1 AND created_at > now() - make_interval(days => $2)
		AND ($3::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($3)))
		GROUP BY category ORDER BY a DESC`, viewer.OrgID, days, deps)
}

func (s *Store) SignaturesByDepartment(ctx context.Context, viewer domain.User, days int) ([]domain.Bar, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Bar](ctx, s.db, `SELECT coalesce(d.name, 'Без подразделения') AS label, count(*) AS a, count(x.signed_at) AS b
		FROM induction_assignments x JOIN users u ON u.id = x.user_id LEFT JOIN departments d ON d.id = u.department_id
		WHERE x.org_id = $1 AND x.assigned_at > now() - make_interval(days => $2)
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND x.instructor_id = $5))
		GROUP BY d.name ORDER BY d.name`, viewer.OrgID, days, deps, viewer.Role, viewer.ID)
}

func (s *Store) OverdueHazards(ctx context.Context, viewer domain.User, limit int) ([]domain.Hazard, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Hazard](ctx, s.db, hazardSelect+` WHERE h.org_id = $1 AND h.status IN ('open', 'in_progress')
		AND h.due_at < current_date AND ($3::bigint[] IS NULL OR EXISTS (SELECT 1 FROM users hu WHERE hu.id IN (h.author_id, h.responsible_id) AND hu.department_id = ANY($3)))
		ORDER BY h.due_at LIMIT $2`, viewer.OrgID, limit, deps)
}

func (s *Store) OverdueAssignments(ctx context.Context, viewer domain.User, limit int) ([]domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.signed_at IS NULL AND a.due_at < current_date
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5))
		ORDER BY a.due_at LIMIT $2`, viewer.OrgID, limit, deps, viewer.Role, viewer.ID)
}

func (s *Store) ExpiringRepeats(ctx context.Context, viewer domain.User, days int) ([]domain.Expiring, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Expiring](ctx, s.db, `SELECT * FROM (
		SELECT DISTINCT ON (a.user_id, a.template_id) a.user_id, u.name AS user_name, a.template_id, t.title,
			(a.signed_at + make_interval(months => t.repeat_months))::date AS next_due
		FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
		JOIN induction_records ir ON ir.assignment_id=a.id AND ir.instructor_signed_at IS NOT NULL
		WHERE a.org_id = $1 AND a.signed_at IS NOT NULL AND a.knowledge_result='passed' AND t.repeat_months > 0 AND u.active
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5))
		ORDER BY a.user_id, a.template_id, a.signed_at DESC) x
		WHERE x.next_due <= current_date + $2::int
		AND NOT EXISTS (SELECT 1 FROM induction_assignments p WHERE p.user_id = x.user_id AND p.template_id = x.template_id AND p.signed_at IS NULL)
		ORDER BY x.next_due`, viewer.OrgID, days, deps, viewer.Role, viewer.ID)
}

func (s *Store) UserHazardCounts(ctx context.Context, orgID, userID int64) (dashboard.HazardCounts, error) {
	var c dashboard.HazardCounts
	err := s.db.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status IN ('open', 'in_progress')),
		count(*) FILTER (WHERE status = 'fixed'),
		count(*) FILTER (WHERE responsible_id = $2 AND status IN ('open', 'in_progress'))
		FROM hazards WHERE org_id = $1 AND (author_id = $2 OR responsible_id = $2)`, orgID, userID).Scan(&c.Open, &c.Fixed, &c.AssignedToMe)
	return c, err
}
