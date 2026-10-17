package postgres

import (
	"context"
	"time"

	"qauipsiz/internal/domain"
	"qauipsiz/internal/usecase/auth"
)

func (s *Store) ActiveUserExists(ctx context.Context, phone string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT exists(SELECT 1 FROM users WHERE phone = $1 AND active)`, phone).Scan(&ok)
	return ok, err
}

func (s *Store) LastCodeAt(ctx context.Context, phone string) (*time.Time, error) {
	var t *time.Time
	err := s.db.QueryRow(ctx, `SELECT max(created_at) FROM otp_codes WHERE phone = $1`, phone).Scan(&t)
	return t, err
}

func (s *Store) CodesSince(ctx context.Context, phone string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM otp_codes WHERE phone = $1 AND created_at >= $2`, phone, since).Scan(&n)
	return n, err
}

func (s *Store) SaveCode(ctx context.Context, phone, hash string, expires time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO otp_codes (phone, code_hash, expires_at) VALUES ($1, $2, $3)`, phone, hash, expires)
	return err
}

func (s *Store) ActiveCode(ctx context.Context, phone string, now time.Time) (auth.Code, error) {
	var c auth.Code
	err := s.db.QueryRow(ctx, `SELECT id, code_hash, attempts FROM otp_codes
		WHERE phone = $1 AND NOT used AND expires_at > $2 ORDER BY created_at DESC, id DESC LIMIT 1`, phone, now).Scan(&c.ID, &c.Hash, &c.Attempts)
	return c, notFound(err)
}

func (s *Store) IncAttempts(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

func (s *Store) UseCode(ctx context.Context, id int64) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE otp_codes SET used = true WHERE id = $1 AND NOT used`, id)
	return tag.RowsAffected() == 1, err
}

func (s *Store) CreateSession(ctx context.Context, phone, tokenHash string, expires time.Time) (domain.User, error) {
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at)
		SELECT $1, id, $3 FROM users WHERE phone = $2 AND active RETURNING user_id`, tokenHash, phone, expires).Scan(&id); err != nil {
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
