package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"qauipsiz/internal/domain"
)

func (s *Store) People(ctx context.Context, orgID int64, f domain.PeopleFilter) ([]domain.User, error) {
	return list[domain.User](ctx, s.db, `SELECT `+userCols+`,
		exists(SELECT 1 FROM induction_assignments x WHERE x.user_id = u.id AND x.signed_at IS NULL AND x.due_at < current_date) AS overdue
		FROM users u `+userJoins+` WHERE u.org_id = $1
		AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.phone LIKE '%' || $2 || '%' OR u.position ILIKE '%' || $2 || '%')
		AND ($3 = '' OR u.department_id::text = $3) AND ($4 = '' OR u.location_id::text = $4)
		ORDER BY u.active DESC, u.name`, orgID, f.Query, f.Department, f.Location)
}

func (s *Store) Person(ctx context.Context, orgID, id int64) (domain.User, error) {
	return one[domain.User](ctx, s.db, `SELECT `+userCols+` FROM users u `+userJoins+` WHERE u.id = $1 AND u.org_id = $2`, id, orgID)
}

// ownRef — id подразделения/объекта, только если он принадлежит организации.
func ownRef(ctx context.Context, q pgx.Tx, table string, orgID int64, id *int64) (*int64, error) {
	if id == nil {
		return nil, nil
	}
	var out *int64
	err := q.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id = $1 AND org_id = $2`, *id, orgID).Scan(&out)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return out, err
}

func (s *Store) savePerson(ctx context.Context, orgID int64, u domain.User, insert bool) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		dep, err := ownRef(ctx, tx, "departments", orgID, u.DepartmentID)
		if err != nil {
			return err
		}
		loc, err := ownRef(ctx, tx, "locations", orgID, u.LocationID)
		if err != nil {
			return err
		}
		args := []any{orgID, u.Phone, u.Name, u.BirthYear, u.Position, dep, loc, u.Role, u.Active}
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO users (org_id, phone, name, birth_year, position, department_id, location_id, role, active)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, args...)
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET phone=$2, name=$3, birth_year=$4, position=$5, department_id=$6, location_id=$7, role=$8, active=$9
			WHERE org_id=$1 AND id=$10`, append(args, u.ID)...)
		if err == nil && tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return err
	})
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}
	return err
}

func (s *Store) CreatePerson(ctx context.Context, orgID int64, u domain.User) error {
	return s.savePerson(ctx, orgID, u, true)
}

func (s *Store) UpdatePerson(ctx context.Context, orgID int64, u domain.User) error {
	return s.savePerson(ctx, orgID, u, false)
}

func (s *Store) ImportPeople(ctx context.Context, orgID int64, rows []domain.ImportRow) (int, []int, error) {
	imported := 0
	var conflicts []int
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		refs := map[string]*int64{}
		ref := func(table, name string) (*int64, error) {
			if name == "" {
				return nil, nil
			}
			key := table + "|" + strings.ToLower(name)
			if id, ok := refs[key]; ok {
				return id, nil
			}
			var id int64
			err := tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE org_id = $1 AND lower(name) = lower($2) ORDER BY id LIMIT 1`, orgID, name).Scan(&id)
			if err == pgx.ErrNoRows {
				err = tx.QueryRow(ctx, `INSERT INTO `+table+` (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, name).Scan(&id)
			}
			if err != nil {
				return nil, err
			}
			refs[key] = &id
			return &id, nil
		}
		for _, r := range rows {
			dep, err := ref("departments", r.Department)
			if err != nil {
				return err
			}
			loc, err := ref("locations", r.Location)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO users (org_id, phone, name, birth_year, position, department_id, location_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				ON CONFLICT (phone) DO UPDATE SET name = $3, birth_year = $4, position = $5, department_id = $6, location_id = $7
				WHERE users.org_id = $1`, orgID, r.Phone, r.Name, r.BirthYear, r.Position, dep, loc)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				conflicts = append(conflicts, r.Line)
				continue
			}
			imported++
		}
		return nil
	})
	return imported, conflicts, err
}

func (s *Store) Staff(ctx context.Context, orgID int64) ([]domain.User, error) {
	return list[domain.User](ctx, s.db, `SELECT id, name, position, role FROM users WHERE org_id = $1 AND active ORDER BY name`, orgID)
}

func (s *Store) Departments(ctx context.Context, orgID int64) ([]domain.Department, error) {
	return list[domain.Department](ctx, s.db, `SELECT d.id, d.parent_id, d.name, coalesce(p.name, '') AS parent,
		(SELECT count(*) FROM users x WHERE x.department_id = d.id AND x.active) AS people
		FROM departments d LEFT JOIN departments p ON p.id = d.parent_id
		WHERE d.org_id = $1 ORDER BY coalesce(p.name, d.name), d.parent_id NULLS FIRST, d.name`, orgID)
}

func (s *Store) SaveDepartment(ctx context.Context, orgID int64, d domain.Department) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		parent, err := ownRef(ctx, tx, "departments", orgID, d.ParentID)
		if err != nil {
			return err
		}
		if d.ID == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO departments (org_id, name, parent_id) VALUES ($1, $2, $3)`, orgID, d.Name, parent)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE departments SET name = $3, parent_id = $4 WHERE id = $1 AND org_id = $2`, d.ID, orgID, d.Name, parent)
		return err
	})
}

func (s *Store) DeleteDepartment(ctx context.Context, orgID, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM departments WHERE id = $1 AND org_id = $2`, id, orgID)
	return err
}

func (s *Store) Locations(ctx context.Context, orgID int64) ([]domain.Location, error) {
	return list[domain.Location](ctx, s.db, `SELECT l.id, l.name, l.address, l.lat, l.lng, l.radius_m, l.responsible_id, l.evacuation_path,
		coalesce(x.name, '') AS responsible FROM locations l LEFT JOIN users x ON x.id = l.responsible_id WHERE l.org_id = $1 ORDER BY l.name`, orgID)
}

func (s *Store) SaveLocation(ctx context.Context, orgID int64, l domain.Location) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var resp *int64
		if l.ResponsibleID != nil {
			if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND org_id = $2`, *l.ResponsibleID, orgID).Scan(&resp); err != nil {
				return domain.Invalid("Неизвестный сотрудник")
			}
		}
		args := []any{orgID, l.Name, l.Address, l.Lat, l.Lng, l.RadiusM, resp, l.EvacuationPath}
		if l.ID == 0 {
			_, err := tx.Exec(ctx, `INSERT INTO locations (org_id, name, address, lat, lng, radius_m, responsible_id, evacuation_path)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, args...)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE locations SET name=$2, address=$3, lat=$4, lng=$5, radius_m=$6, responsible_id=$7, evacuation_path=CASE WHEN $8 = '' THEN evacuation_path ELSE $8 END
			WHERE org_id=$1 AND id=$9`, append(args, l.ID)...)
		return err
	})
}

func (s *Store) SaveOrg(ctx context.Context, o domain.Org) error {
	_, err := s.db.Exec(ctx, `UPDATE orgs SET name = $2, bin = $3, last_injury_at = $4, repeat_warn_days = $5 WHERE id = $1`,
		o.ID, o.Name, o.BIN, o.LastInjuryAt, o.RepeatWarnDays)
	return err
}

func (s *Store) ActiveUsers(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE org_id = $1 AND active`, orgID).Scan(&n)
	return n, err
}
