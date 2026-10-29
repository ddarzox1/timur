package postgres

import (
	"context"
	"time"

	"qauipsiz/internal/domain"
)

const assignmentSelect = `SELECT a.id, a.template_id, a.user_id, a.batch, a.reason, a.location_id, coalesce(al.name, '') AS work_location,
	a.basis, a.work_type, a.docs, a.assigned_at, a.due_at, a.signed_at, a.signature,
	a.selfie_path, a.geo_ok, t.title, t.title_kk, t.kind, u.name AS user_name, u.birth_year, u.position,
	coalesce(d.name, '') AS department, coalesce(i.name, '') AS instructor, coalesce(i.position, '') AS instructor_pos
	FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
	LEFT JOIN departments d ON d.id = u.department_id LEFT JOIN users i ON i.id = a.instructor_id
	LEFT JOIN locations al ON al.id = a.location_id`

func (s *Store) InductionTemplates(ctx context.Context, orgID int64) ([]domain.InductionTemplate, error) {
	return list[domain.InductionTemplate](ctx, s.db, `SELECT t.id, t.title, t.title_kk, t.kind, t.repeat_months, t.instructor_id,
		coalesce(i.name, '') AS instructor, (SELECT count(*) FROM induction_assignments a WHERE a.template_id = t.id) AS assigned
		FROM induction_templates t LEFT JOIN users i ON i.id = t.instructor_id WHERE t.org_id = $1 ORDER BY t.id DESC`, orgID)
}

func (s *Store) InductionTemplate(ctx context.Context, orgID, id int64) (domain.InductionTemplate, error) {
	return one[domain.InductionTemplate](ctx, s.db, `SELECT t.id, t.title, t.title_kk, t.kind, t.repeat_months, t.instructor_id,
		coalesce(i.name, '') AS instructor, t.body, t.file_path, t.require_geo, t.require_selfie
		FROM induction_templates t LEFT JOIN users i ON i.id = t.instructor_id WHERE t.id = $1 AND t.org_id = $2`, id, orgID)
}

func (s *Store) SaveInductionTemplate(ctx context.Context, orgID int64, t domain.InductionTemplate) error {
	args := []any{orgID, t.Title, t.TitleKK, t.Kind, t.RepeatMonths, t.InstructorID, t.Body, t.FilePath, t.RequireGeo, t.RequireSelfie}
	if t.ID == 0 {
		_, err := s.db.Exec(ctx, `INSERT INTO induction_templates (org_id, title, title_kk, kind, repeat_months, instructor_id,
			body, file_path, require_geo, require_selfie) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, args...)
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE induction_templates SET title=$2, title_kk=$3, kind=$4, repeat_months=$5, instructor_id=$6,
		body=$7, file_path=CASE WHEN $8 = '' THEN file_path ELSE $8 END, require_geo=$9, require_selfie=$10 WHERE org_id=$1 AND id=$11`, append(args, t.ID)...)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) UserAssignment(ctx context.Context, userID, id int64) (domain.Assignment, error) {
	return one[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.id = $1 AND a.user_id = $2`, id, userID)
}

func (s *Store) SignAssignment(ctx context.Context, userID, id int64, sig domain.Signature) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now(), signature = $3, selfie_path = $4, lat = $5, lng = $6, geo_ok = $7
		WHERE id = $1 AND user_id = $2 AND signed_at IS NULL`, id, userID, sig.Image, sig.Selfie, sig.Lat, sig.Lng, sig.GeoOK)
	return tag.RowsAffected() == 1, err
}

func (s *Store) UsersForAssignment(ctx context.Context, viewer domain.User, userIDs []int64) ([]domain.User, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.User](ctx, s.db, `SELECT id, org_id, coalesce(phone, '') AS phone, coalesce(iin, '') AS iin,
		name, position, department_id, location_id, role, active FROM users
		WHERE org_id = $1 AND id = ANY($2) AND active AND ($3::bigint[] IS NULL OR department_id = ANY($3))
		ORDER BY id`, viewer.OrgID, userIDs, deps)
}

func (s *Store) CreateAssignments(ctx context.Context, viewer domain.User, templateID int64, instructorID, locationID *int64,
	batch, reason, basis, workType, docs string, due time.Time, userIDs []int64) (int, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return 0, err
	}
	// Сотрудники фильтруются по организации и области видимости.
	tag, err := s.db.Exec(ctx, `INSERT INTO induction_assignments
		(org_id, template_id, user_id, instructor_id, location_id, batch, reason, basis, work_type, docs, due_at)
		SELECT $1, $2, id, $3, $4, $5, $6, $7, $8, $9, $10 FROM users
		WHERE org_id = $1 AND id = ANY($11) AND ($12::bigint[] IS NULL OR department_id = ANY($12))`,
		viewer.OrgID, templateID, instructorID, locationID, batch, reason, basis, workType, docs, due, userIDs, deps)
	return int(tag.RowsAffected()), err
}

func (s *Store) BatchExists(ctx context.Context, viewer domain.User, batch string) (bool, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.db.QueryRow(ctx, `SELECT exists(SELECT 1 FROM induction_assignments a JOIN users u ON u.id = a.user_id
		WHERE a.batch = $1 AND a.org_id = $2 AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5)))`,
		batch, viewer.OrgID, deps, viewer.Role, viewer.ID).Scan(&ok)
	return ok, err
}

func (s *Store) UnsignedInBatch(ctx context.Context, orgID int64, batch string) ([]domain.Notice, error) {
	return list[domain.Notice](ctx, s.db, `SELECT a.id, u.phone, t.title FROM induction_assignments a JOIN users u ON u.id = a.user_id
		JOIN induction_templates t ON t.id = a.template_id WHERE a.org_id = $1 AND a.batch = $2 AND a.signed_at IS NULL AND u.active`, orgID, batch)
}

func (s *Store) Batches(ctx context.Context, viewer domain.User) ([]domain.Batch, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Batch](ctx, s.db, `SELECT a.batch, t.title, t.kind, min(a.assigned_at) AS assigned_at, max(a.due_at) AS due_at,
		count(*) AS total, count(a.signed_at) AS signed
		FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
		WHERE a.org_id = $1 AND ($2::bigint[] IS NULL OR u.department_id = ANY($2) OR ($3 = 'instructor' AND a.instructor_id = $4))
		GROUP BY a.batch, t.title, t.kind ORDER BY min(a.assigned_at) DESC LIMIT 200`, viewer.OrgID, deps, viewer.Role, viewer.ID)
}

func (s *Store) BatchAssignments(ctx context.Context, viewer domain.User, batch string) ([]domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.batch = $2
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5)) ORDER BY u.name`,
		viewer.OrgID, batch, deps, viewer.Role, viewer.ID)
}

func (s *Store) Journal(ctx context.Context, viewer domain.User, kinds []string, f domain.JournalFilter) ([]domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.signed_at IS NOT NULL AND t.kind = ANY($2)
		AND a.signed_at >= coalesce(nullif($3, '')::date, '-infinity') AND a.signed_at < coalesce(nullif($4, '')::date + 1, 'infinity')
		AND ($5 = '' OR u.department_id::text = $5) AND ($6 = '' OR u.name ILIKE '%' || $6 || '%')
		AND ($7::bigint[] IS NULL OR u.department_id = ANY($7) OR ($8 = 'instructor' AND a.instructor_id = $9))
		ORDER BY a.signed_at`, viewer.OrgID, kinds, f.From, f.To, f.Department, f.Name, deps, viewer.Role, viewer.ID)
}

func (s *Store) UserAssignments(ctx context.Context, userID int64, signed bool) ([]domain.Assignment, error) {
	order := `a.due_at`
	if signed {
		order = `a.signed_at DESC`
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.user_id = $1 AND (a.signed_at IS NOT NULL) = $2 ORDER BY `+order, userID, signed)
}
