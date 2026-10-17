// Package postgres — реализация репозиториев usecase на PostgreSQL (pgx).
package postgres

import (
	"context"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"qauipsiz/internal/domain"
	"qauipsiz/migrations"
)

// Connect открывает пул и применяет миграции.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	sub, _ := fs.Sub(migrations.FS, ".")
	goose.SetBaseFS(sub)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Store реализует интерфейсы Repo всех usecase поверх одного пула.
type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func list[T any](ctx context.Context, q querier, sql string, args ...any) ([]T, error) {
	rows, _ := q.Query(ctx, sql, args...)
	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
}

func one[T any](ctx context.Context, q querier, sql string, args ...any) (T, error) {
	rows, _ := q.Query(ctx, sql, args...)
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	return v, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

const userCols = `u.id, u.org_id, u.phone, u.name, u.birth_year, u.position, u.department_id, u.location_id, u.role, u.active,
	coalesce(d.name, '') AS department, coalesce(l.name, '') AS location`
const userJoins = `LEFT JOIN departments d ON d.id = u.department_id LEFT JOIN locations l ON l.id = u.location_id`
