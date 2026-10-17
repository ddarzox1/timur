package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"qauipsiz/internal/domain"
)

const hazardSelect = `SELECT h.id, h.org_id, h.author_id, h.location_id, h.place, h.category, h.risk, h.description, h.responsible_id,
	h.due_at, h.status, h.action, h.created_at, h.fixed_at,
	coalesce(a.name, '') AS author, coalesce(l.name, '') AS location, l.lat, l.lng, coalesce(r.name, '') AS responsible,
	coalesce((SELECT path FROM hazard_photos p WHERE p.hazard_id = h.id AND p.kind = 'before' ORDER BY id LIMIT 1), '') AS photo
	FROM hazards h LEFT JOIN users a ON a.id = h.author_id LEFT JOIN locations l ON l.id = h.location_id
	LEFT JOIN users r ON r.id = h.responsible_id`

func (s *Store) CreateHazard(ctx context.Context, h domain.Hazard, photos []string) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO hazards (org_id, author_id, location_id, place, category, risk, description, responsible_id, due_at, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			h.OrgID, h.AuthorID, h.LocationID, h.Place, h.Category, h.Risk, h.Description, h.ResponsibleID, h.DueAt, h.Status).Scan(&id); err != nil {
			return err
		}
		for _, p := range photos {
			if _, err := tx.Exec(ctx, `INSERT INTO hazard_photos (hazard_id, kind, path) VALUES ($1, 'before', $2)`, id, p); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO hazard_events (hazard_id, user_id, text) VALUES ($1, $2, 'Создано')`, id, h.AuthorID)
		return err
	})
	return id, err
}

func (s *Store) Hazard(ctx context.Context, orgID, id int64) (domain.Hazard, error) {
	return one[domain.Hazard](ctx, s.db, hazardSelect+` WHERE h.id = $1 AND h.org_id = $2`, id, orgID)
}

func (s *Store) Hazards(ctx context.Context, orgID int64, f domain.HazardFilter) ([]domain.Hazard, error) {
	return list[domain.Hazard](ctx, s.db, hazardSelect+` WHERE h.org_id = $1
		AND ($2 = '' OR h.status = $2 OR ($2 = 'active' AND h.status IN ('open', 'in_progress'))
			OR ($2 = 'overdue' AND h.status IN ('open', 'in_progress') AND h.due_at < current_date))
		AND ($3 = '' OR h.risk = $3) AND ($4 = '' OR h.category = $4)
		AND ($5 = '' OR h.location_id::text = $5) AND ($6 = '' OR h.responsible_id::text = $6)
		AND h.created_at >= coalesce(nullif($7, '')::date, '-infinity') AND h.created_at < coalesce(nullif($8, '')::date + 1, 'infinity')
		ORDER BY h.id DESC LIMIT 500`, orgID, f.Status, f.Risk, f.Category, f.Location, f.Responsible, f.From, f.To)
}

func (s *Store) UserHazards(ctx context.Context, orgID, userID int64, closed bool) ([]domain.Hazard, error) {
	return list[domain.Hazard](ctx, s.db, hazardSelect+` WHERE h.org_id = $1 AND (h.author_id = $2 OR h.responsible_id = $2)
		AND (h.status IN ('fixed', 'rejected')) = $3 ORDER BY h.created_at DESC LIMIT 100`, orgID, userID, closed)
}

func (s *Store) HazardPhotos(ctx context.Context, hazardID int64) ([]domain.Photo, error) {
	return list[domain.Photo](ctx, s.db, `SELECT kind, path FROM hazard_photos WHERE hazard_id = $1 ORDER BY id`, hazardID)
}

func (s *Store) HazardEvents(ctx context.Context, hazardID int64) ([]domain.Event, error) {
	return list[domain.Event](ctx, s.db, `SELECT e.text, e.created_at, coalesce(u.name, 'Аноним') AS user_name
		FROM hazard_events e LEFT JOIN users u ON u.id = e.user_id WHERE e.hazard_id = $1 ORDER BY e.id`, hazardID)
}

func (s *Store) UpdateHazard(ctx context.Context, h domain.Hazard, byUser int64, events []string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE hazards SET responsible_id = $3, due_at = $4, risk = $5, status = $6, action = $7, fixed_at = $8
			WHERE id = $1 AND org_id = $2`, h.ID, h.OrgID, h.ResponsibleID, h.DueAt, h.Risk, h.Status, h.Action, h.FixedAt); err != nil {
			return err
		}
		for _, e := range events {
			if _, err := tx.Exec(ctx, `INSERT INTO hazard_events (hazard_id, user_id, text) VALUES ($1, $2, $3)`, h.ID, byUser, e); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) FixHazard(ctx context.Context, orgID, hazardID, byUser int64, comment, photo string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE hazards SET status = 'fixed', fixed_at = now(), action = $3 WHERE id = $1 AND org_id = $2`, hazardID, orgID, comment)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `INSERT INTO hazard_photos (hazard_id, kind, path) VALUES ($1, 'after', $2)`, hazardID, photo); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO hazard_events (hazard_id, user_id, text) VALUES ($1, $2, $3)`, hazardID, byUser, "Устранено: "+comment)
		return err
	})
}

func (s *Store) AddHazardEvent(ctx context.Context, hazardID int64, userID *int64, text string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO hazard_events (hazard_id, user_id, text) VALUES ($1, $2, $3)`, hazardID, userID, text)
	return err
}

func (s *Store) AssignHazards(ctx context.Context, orgID int64, ids []int64, responsibleID int64) ([]int64, error) {
	rows, _ := s.db.Query(ctx, `UPDATE hazards SET responsible_id = $3 WHERE org_id = $1 AND id = ANY($2) RETURNING id`, orgID, ids, responsibleID)
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
