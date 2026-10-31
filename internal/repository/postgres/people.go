package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"qauipsiz/internal/domain"
)

func (s *Store) People(ctx context.Context, viewer domain.User, f domain.PeopleFilter) ([]domain.User, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.User](ctx, s.db, `SELECT `+userCols+`,
		exists(SELECT 1 FROM induction_assignments x WHERE x.user_id = u.id AND x.signed_at IS NULL AND x.due_at < current_date) AS overdue
		FROM users u `+userJoins+` WHERE u.org_id = $1 AND ($5::bigint[] IS NULL OR u.department_id = ANY($5))
		AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR coalesce(u.phone, '') LIKE '%' || $2 || '%' OR coalesce(u.iin, '') LIKE '%' || $2 || '%' OR u.position ILIKE '%' || $2 || '%')
		AND ($3 = '' OR u.department_id::text = $3) AND ($4 = '' OR u.location_id::text = $4)
		ORDER BY u.active DESC, u.name`, viewer.OrgID, f.Query, f.Department, f.Location, deps)
}

func (s *Store) Person(ctx context.Context, viewer domain.User, id int64) (domain.User, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return domain.User{}, err
	}
	return one[domain.User](ctx, s.db, `SELECT `+userCols+` FROM users u `+userJoins+`
		WHERE u.id = $1 AND u.org_id = $2 AND ($3::bigint[] IS NULL OR u.department_id = ANY($3))`, id, viewer.OrgID, deps)
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
		var phone, iin any
		if u.Phone != "" {
			phone = u.Phone
		}
		if u.IIN != "" {
			iin = u.IIN
		}
		args := []any{orgID, phone, iin, u.Name, u.BirthYear, u.Position, dep, loc, u.Role, u.Active}
		if insert {
			_, err = tx.Exec(ctx, `INSERT INTO users (org_id, phone, iin, name, birth_year, position, department_id, location_id, role, active)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, args...)
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET phone=$2, iin=$3, name=$4, birth_year=$5, position=$6, department_id=$7, location_id=$8, role=$9, active=$10
			WHERE org_id=$1 AND id=$11`, append(args, u.ID)...)
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

func (s *Store) CreatePerson(ctx context.Context, orgID int64, u domain.User, passwordHash string) error {
	if passwordHash == "" {
		return s.savePerson(ctx, orgID, u, true)
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		dep, err := ownRef(ctx, tx, "departments", orgID, u.DepartmentID)
		if err != nil {
			return err
		}
		loc, err := ownRef(ctx, tx, "locations", orgID, u.LocationID)
		if err != nil {
			return err
		}
		var phone any
		if u.Phone != "" {
			phone = u.Phone
		}
		_, err = tx.Exec(ctx, `INSERT INTO users (org_id, phone, iin, name, birth_year, position, department_id, location_id, role, active, password_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, orgID, phone, u.IIN, u.Name, u.BirthYear, u.Position, dep, loc, u.Role, u.Active, passwordHash)
		return err
	})
	if isUniqueViolation(err) {
		return domain.ErrConflict
	}
	return err
}

func (s *Store) UpdatePerson(ctx context.Context, orgID int64, u domain.User) error {
	return s.savePerson(ctx, orgID, u, false)
}

func (s *Store) ResetPassword(ctx context.Context, orgID, userID int64, passwordHash string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash=$3 WHERE org_id=$1 AND id=$2`, orgID, userID, passwordHash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
		return err
	})
}

func (s *Store) ImportPeople(ctx context.Context, orgID int64, rows []domain.ImportRow) (int, []int, []int, error) {
	imported := 0
	var conflicts []int
	var created []int
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
			var inserted bool
			err = tx.QueryRow(ctx, `INSERT INTO users (org_id, phone, iin, name, birth_year, position, department_id, location_id, password_hash)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				ON CONFLICT (iin) WHERE iin IS NOT NULL AND active AND password_hash <> '' DO UPDATE SET phone = $2, name = $4, birth_year = $5, position = $6, department_id = $7, location_id = $8
				WHERE users.org_id = $1
				RETURNING (xmax = 0)`, orgID, r.Phone, r.IIN, r.Name, r.BirthYear, r.Position, dep, loc, r.PasswordHash).Scan(&inserted)
			if err == pgx.ErrNoRows {
				conflicts = append(conflicts, r.Line)
				continue
			}
			if err != nil {
				return err
			}
			if inserted {
				created = append(created, r.Line)
			}
			imported++
		}
		return nil
	})
	if isUniqueViolation(err) {
		return imported, conflicts, created, domain.ErrConflict
	}
	return imported, conflicts, created, err
}

func (s *Store) Staff(ctx context.Context, viewer domain.User) ([]domain.User, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.User](ctx, s.db, `SELECT id, name, position, role FROM users
		WHERE org_id = $1 AND active AND ($2::bigint[] IS NULL OR department_id = ANY($2)) ORDER BY name`, viewer.OrgID, deps)
}

func (s *Store) Departments(ctx context.Context, viewer domain.User) ([]domain.Department, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Department](ctx, s.db, `SELECT d.id, d.parent_id, d.name, coalesce(p.name, '') AS parent,
		(SELECT count(*) FROM users x WHERE x.department_id = d.id AND x.active) AS people
		FROM departments d LEFT JOIN departments p ON p.id = d.parent_id
		WHERE d.org_id = $1 AND ($2::bigint[] IS NULL OR d.id = ANY($2))
		ORDER BY coalesce(p.name, d.name), d.parent_id NULLS FIRST, d.name`, viewer.OrgID, deps)
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
