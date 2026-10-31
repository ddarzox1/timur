package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/auth"
)

// ProvisionedPassword is emitted only while upgrading legacy users that do not
// yet have a password. The clear text is never stored in PostgreSQL.
type ProvisionedPassword struct {
	UserID   int64
	Name     string
	IIN      string
	Password string
}

// ProvisionInitialPasswords gives legacy active employees a random first
// password after the password_hash migration. A conditional update makes this
// safe to retry after an interrupted startup.
func ProvisionInitialPasswords(ctx context.Context, db *pgxpool.Pool) ([]ProvisionedPassword, []int64, error) {
	rows, err := db.Query(ctx, `SELECT id, name, coalesce(iin, '') FROM users
		WHERE active AND role <> 'visitor' AND password_hash = '' ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	type legacyUser struct {
		id        int64
		name, iin string
	}
	var users []legacyUser
	for rows.Next() {
		var u legacyUser
		if err := rows.Scan(&u.id, &u.name, &u.iin); err != nil {
			rows.Close()
			return nil, nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	var provisioned []ProvisionedPassword
	var withoutIIN []int64
	iinCounts := make(map[string]int, len(users))
	for _, u := range users {
		iinCounts[u.iin]++
	}
	for _, u := range users {
		if !domain.ValidIIN(u.iin) || iinCounts[u.iin] != 1 {
			withoutIIN = append(withoutIIN, u.id)
			continue
		}
		var occupied bool
		if err := db.QueryRow(ctx, `SELECT exists(SELECT 1 FROM users WHERE iin=$1 AND active AND password_hash<>'')`, u.iin).Scan(&occupied); err != nil {
			return nil, withoutIIN, err
		}
		if occupied {
			withoutIIN = append(withoutIIN, u.id)
			continue
		}
		password, err := auth.NewInitialPassword()
		if err != nil {
			return nil, withoutIIN, err
		}
		passwordHash, err := auth.HashPassword(password)
		if err != nil {
			return nil, withoutIIN, err
		}
		tag, err := db.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1 AND password_hash=''`, u.id, passwordHash)
		if err != nil {
			return nil, withoutIIN, err
		}
		if tag.RowsAffected() == 1 {
			provisioned = append(provisioned, ProvisionedPassword{UserID: u.id, Name: u.name, IIN: u.iin, Password: password})
		}
	}
	return provisioned, withoutIIN, nil
}
