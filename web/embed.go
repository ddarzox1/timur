// Package web содержит HTML-шаблоны и статику, встроенные в бинарник.
package web

import "embed"

//go:embed templates static
var FS embed.FS
