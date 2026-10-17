package postgres

import (
	"context"

	"qauipsiz/internal/domain"
)

// Справочные запросы, общие для usecase. Всё фильтруется по org_id.

func (s *Store) Location(ctx context.Context, orgID, id int64) (domain.Location, error) {
	return one[domain.Location](ctx, s.db, `SELECT l.id, l.name, l.address, l.lat, l.lng, l.radius_m, l.responsible_id, l.evacuation_path,
		coalesce(x.name, '') AS responsible FROM locations l LEFT JOIN users x ON x.id = l.responsible_id
		WHERE l.id = $1 AND l.org_id = $2`, id, orgID)
}

func (s *Store) UserName(ctx context.Context, orgID, id int64) (string, error) {
	var name string
	err := s.db.QueryRow(ctx, `SELECT name FROM users WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&name)
	return name, notFound(err)
}

func (s *Store) Org(ctx context.Context, id int64) (domain.Org, error) {
	return one[domain.Org](ctx, s.db, `SELECT id, name, bin, last_injury_at, repeat_warn_days, created_at FROM orgs WHERE id = $1`, id)
}

func (s *Store) SavePilotRequest(ctx context.Context, company, name, phone, employees string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO pilot_requests (company, name, phone, employees) VALUES ($1, $2, $3, $4)`, company, name, phone, employees)
	return err
}
