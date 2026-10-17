package domain

import "strings"

// FormatPhone показывает номер хранения 77024025282 как +7 702 402 52 82 (как маска на фронте).
// Значение не в формате хранения возвращается как есть.
func FormatPhone(p string) string {
	if len(p) != 11 || p[0] != '7' || strings.Trim(p, "0123456789") != "" {
		return p
	}
	return "+7 " + p[1:4] + " " + p[4:7] + " " + p[7:9] + " " + p[9:11]
}

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
