package postgres

import (
	"context"
	"encoding/json"

	"qauipsiz/internal/domain"
)

func (s *Store) ChecklistTemplates(ctx context.Context, orgID int64) ([]domain.ChecklistTemplate, error) {
	return list[domain.ChecklistTemplate](ctx, s.db, `SELECT t.id, t.title, t.location_id, t.frequency, t.items, coalesce(l.name, '') AS location,
		(SELECT count(*) FROM checklist_runs x WHERE x.template_id = t.id) AS runs
		FROM checklist_templates t LEFT JOIN locations l ON l.id = t.location_id WHERE t.org_id = $1 ORDER BY t.title`, orgID)
}

func (s *Store) UserChecklists(ctx context.Context, orgID, userID int64, locationID *int64) ([]domain.ChecklistTemplate, error) {
	return list[domain.ChecklistTemplate](ctx, s.db, `SELECT t.id, t.title, t.location_id, t.frequency, t.items, coalesce(l.name, '') AS location,
		exists(SELECT 1 FROM checklist_runs x WHERE x.template_id = t.id AND x.user_id = $2 AND x.created_at >= current_date) AS done_today
		FROM checklist_templates t LEFT JOIN locations l ON l.id = t.location_id
		WHERE t.org_id = $1 AND (t.location_id IS NULL OR t.location_id = $3) ORDER BY t.title`, orgID, userID, locationID)
}

func (s *Store) ChecklistTemplate(ctx context.Context, orgID, id int64) (domain.ChecklistTemplate, error) {
	return one[domain.ChecklistTemplate](ctx, s.db, `SELECT t.id, t.title, t.location_id, t.frequency, t.items, coalesce(l.name, '') AS location
		FROM checklist_templates t LEFT JOIN locations l ON l.id = t.location_id WHERE t.id = $1 AND t.org_id = $2`, id, orgID)
}

func (s *Store) SaveChecklistTemplate(ctx context.Context, orgID int64, t domain.ChecklistTemplate) error {
	items, err := json.Marshal(t.Items)
	if err != nil {
		return err
	}
	if t.ID == 0 {
		_, err = s.db.Exec(ctx, `INSERT INTO checklist_templates (org_id, title, location_id, frequency, items) VALUES ($1,$2,$3,$4,$5)`,
			orgID, t.Title, t.LocationID, t.Frequency, items)
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE checklist_templates SET title=$3, location_id=$4, frequency=$5, items=$6 WHERE id=$1 AND org_id=$2`,
		t.ID, orgID, t.Title, t.LocationID, t.Frequency, items)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) SaveRun(ctx context.Context, orgID int64, r domain.Run) error {
	answers, err := json.Marshal(r.Answers)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO checklist_runs (org_id, template_id, user_id, location_id, answers, issues, total, signature)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, orgID, r.TemplateID, r.UserID, r.LocationID, answers, r.Issues, r.Total, r.Signature)
	return err
}

const runSelect = `SELECT x.id, x.template_id, x.user_id, x.location_id, t.title, coalesce(l.name, '') AS location,
	coalesce(u.name, '') AS user_name, x.answers, x.issues, x.total, x.signature, x.created_at
	FROM checklist_runs x JOIN checklist_templates t ON t.id = x.template_id
	LEFT JOIN locations l ON l.id = x.location_id LEFT JOIN users u ON u.id = x.user_id`

func (s *Store) Runs(ctx context.Context, orgID int64) ([]domain.Run, error) {
	return list[domain.Run](ctx, s.db, runSelect+` WHERE x.org_id = $1 ORDER BY x.id DESC LIMIT 300`, orgID)
}

func (s *Store) Run(ctx context.Context, orgID, id int64) (domain.Run, error) {
	return one[domain.Run](ctx, s.db, runSelect+` WHERE x.id = $1 AND x.org_id = $2`, id, orgID)
}
