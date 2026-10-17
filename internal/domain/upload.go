package domain

import "io"

// Upload — файл от пользователя; delivery открывает его, usecase решает, сохранять ли.
type Upload struct {
	Name    string
	Size    int64
	Content io.ReadSeeker
}

// Bar — точка графика на дашборде.
type Bar struct {
	Label string `db:"label"`
	A     int    `db:"a"`
	B     int    `db:"b"`
}
