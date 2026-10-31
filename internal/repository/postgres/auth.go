package postgres

import (
	"context"
	"time"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/auth"
)

func (s *Store) LoginCredential(ctx context.Context, iin string) (auth.Credential, error) {
	var passwordHash string
	u, err := one[domain.User](ctx, s.db, `SELECT `+userCols+`, o.name AS org_name FROM users u
		JOIN orgs o ON o.id = u.org_id `+userJoins+` WHERE u.iin = $1 AND u.active AND u.password_hash <> ''`, iin)
	if err != nil {
		return auth.Credential{}, err
	}
	if err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, u.ID).Scan(&passwordHash); err != nil {
		return auth.Credential{}, err
	}
	return auth.Credential{User: u, PasswordHash: passwordHash}, nil
}

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, expires time.Time) (domain.User, error) {
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at)
		SELECT $1, id, $3 FROM users WHERE id = $2 AND active RETURNING user_id`, tokenHash, userID, expires).Scan(&id); err != nil {
		return domain.User{}, notFound(err)
	}
	return one[domain.User](ctx, s.db, `SELECT `+userCols+`, o.name AS org_name FROM users u JOIN orgs o ON o.id = u.org_id `+userJoins+` WHERE u.id = $1`, id)
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string, now time.Time) (domain.User, error) {
	return one[domain.User](ctx, s.db, `SELECT `+userCols+`, o.name AS org_name FROM sessions s
		JOIN users u ON u.id = s.user_id JOIN orgs o ON o.id = u.org_id `+userJoins+`
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.active`, tokenHash, now)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) PasswordHash(ctx context.Context, userID int64) (string, error) {
	var passwordHash string
	err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1 AND active`, userID).Scan(&passwordHash)
	return passwordHash, notFound(err)
}

func (s *Store) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	tag, err := s.db.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1 AND active`, userID, passwordHash)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) TouchLastSeen(ctx context.Context, userID int64, before time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET last_seen_at=now() WHERE id=$1 AND (last_seen_at IS NULL OR last_seen_at < $2)`, userID, before)
	return err
}
