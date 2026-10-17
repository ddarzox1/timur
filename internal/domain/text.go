package domain

import "strings"

// Ограничения длины пользовательского текста (в символах).
const (
	MaxTitle = 200
	MaxText  = 2000
	MaxBody  = 50000 // текст инструктажа
)

// Clip обрезает пробелы по краям и ограничивает длину в символах (не байтах).
func Clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max]))
	}
	return s
}
