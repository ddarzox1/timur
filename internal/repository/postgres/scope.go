package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"qauipsiz/internal/domain"
)

// scopeDepts возвращает nil для доступа ко всей организации и список допустимых
// подразделений для ограниченных ролей. Пустой список означает отсутствие области.
func (s *Store) scopeDepts(ctx context.Context, u domain.User) ([]int64, error) {
	if u.SeesOrg() {
		return nil, nil
	}
	if u.DepartmentID == nil {
		return []int64{}, nil
	}
	if u.Role != domain.RoleShopHead {
		return []int64{*u.DepartmentID}, nil
	}
	rows, err := s.db.Query(ctx, `WITH RECURSIVE scoped AS (
		SELECT id FROM departments WHERE id = $1 AND org_id = $2
		UNION ALL
		SELECT d.id FROM departments d JOIN scoped p ON d.parent_id = p.id WHERE d.org_id = $2
	) SELECT id FROM scoped`, *u.DepartmentID, u.OrgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
