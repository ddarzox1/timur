package postgres

import (
	"context"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/dashboard"
)

func (s *Store) DashboardCounts(ctx context.Context, orgID int64, days int) (dashboard.Counts, error) {
	var c dashboard.Counts
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM hazards WHERE org_id = $1 AND status IN ('open', 'in_progress')),
		(SELECT count(*) FROM hazards WHERE org_id = $1 AND status IN ('open', 'in_progress') AND due_at < current_date),
		(SELECT count(*) FROM induction_assignments WHERE org_id = $1 AND assigned_at > now() - make_interval(days => $2)),
		(SELECT count(signed_at) FROM induction_assignments WHERE org_id = $1 AND assigned_at > now() - make_interval(days => $2)),
		(SELECT count(*) FROM checklist_runs WHERE org_id = $1 AND created_at > now() - interval '7 days')`, orgID, days).
		Scan(&c.OpenHazards, &c.OverdueHazards, &c.Assigned, &c.Signed, &c.RunsWeek)
	return c, err
}

func (s *Store) HazardsByWeek(ctx context.Context, orgID int64) ([]domain.Bar, error) {
	return list[domain.Bar](ctx, s.db, `SELECT to_char(w, 'DD.MM') AS label,
		(SELECT count(*) FROM hazards WHERE org_id = $1 AND created_at >= w AND created_at < w + interval '7 days') AS a,
		(SELECT count(*) FROM hazards WHERE org_id = $1 AND fixed_at >= w AND fixed_at < w + interval '7 days') AS b
		FROM generate_series(date_trunc('week', now()) - interval '7 weeks', date_trunc('week', now()), interval '1 week') w`, orgID)
}

func (s *Store) HazardsByCategory(ctx context.Context, orgID int64, days int) ([]domain.Bar, error) {
	return list[domain.Bar](ctx, s.db, `SELECT category AS label, count(*) AS a, count(*) FILTER (WHERE status IN ('open', 'in_progress')) AS b
		FROM hazards WHERE org_id = $1 AND created_at > now() - make_interval(days => $2) GROUP BY category ORDER BY a DESC`, orgID, days)
}

func (s *Store) SignaturesByDepartment(ctx context.Context, orgID int64, days int) ([]domain.Bar, error) {
	return list[domain.Bar](ctx, s.db, `SELECT coalesce(d.name, 'Без подразделения') AS label, count(*) AS a, count(x.signed_at) AS b
		FROM induction_assignments x JOIN users u ON u.id = x.user_id LEFT JOIN departments d ON d.id = u.department_id
		WHERE x.org_id = $1 AND x.assigned_at > now() - make_interval(days => $2) GROUP BY d.name ORDER BY d.name`, orgID, days)
}

func (s *Store) OverdueHazards(ctx context.Context, orgID int64, limit int) ([]domain.Hazard, error) {
	return list[domain.Hazard](ctx, s.db, hazardSelect+` WHERE h.org_id = $1 AND h.status IN ('open', 'in_progress')
		AND h.due_at < current_date ORDER BY h.due_at LIMIT $2`, orgID, limit)
}

func (s *Store) OverdueAssignments(ctx context.Context, orgID int64, limit int) ([]domain.Assignment, error) {
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.signed_at IS NULL AND a.due_at < current_date
		ORDER BY a.due_at LIMIT $2`, orgID, limit)
}

func (s *Store) ExpiringRepeats(ctx context.Context, orgID int64, days int) ([]domain.Expiring, error) {
	return list[domain.Expiring](ctx, s.db, `SELECT * FROM (
		SELECT DISTINCT ON (a.user_id, a.template_id) a.user_id, u.name AS user_name, a.template_id, t.title,
			(a.signed_at + make_interval(months => t.repeat_months))::date AS next_due
		FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
		WHERE a.org_id = $1 AND a.signed_at IS NOT NULL AND t.repeat_months > 0 AND u.active
		ORDER BY a.user_id, a.template_id, a.signed_at DESC) x
		WHERE x.next_due <= current_date + $2::int
		AND NOT EXISTS (SELECT 1 FROM induction_assignments p WHERE p.user_id = x.user_id AND p.template_id = x.template_id AND p.signed_at IS NULL)
		ORDER BY x.next_due`, orgID, days)
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
