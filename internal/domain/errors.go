// Package domain — сущности и бизнес-правила Qauipsiz без зависимостей от БД и HTTP.
package domain

import "errors"

var (
	ErrNotFound  = errors.New("не найдено")
	ErrForbidden = errors.New("нет доступа")
	// ErrConflict — нарушение уникальности (например, телефон уже занят).
	ErrConflict = errors.New("уже существует")
)

// ValidationError — ошибка, текст которой можно показать пользователю.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func Invalid(msg string) error { return ValidationError(msg) }
