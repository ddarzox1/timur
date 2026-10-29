package postgres

import (
	"context"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/dashboard"
)

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
		WHERE a.org_id = $1 AND a.signed_at IS NOT NULL AND t.repeat_months > 0 AND u.active
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
