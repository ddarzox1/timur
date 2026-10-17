package domain

import "strings"

// NormalizePhone приводит +7 (701) 000-00-01 / 8701… / 701… к виду 77010000001.
// Возвращает "", если номер не казахстанский мобильный формата +7.
func NormalizePhone(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	d := b.String()
	if len(d) == 10 {
		d = "7" + d
	}
	if len(d) == 11 && d[0] == '8' {
		d = "7" + d[1:]
	}
	if len(d) != 11 || d[0] != '7' {
		return ""
	}
	return d
}
