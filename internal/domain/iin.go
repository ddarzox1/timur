package domain

import "strings"

// NormalizeIIN убирает пробелы; остальные символы оставляет, чтобы валидация их отклонила.
func NormalizeIIN(v string) string { return strings.ReplaceAll(strings.TrimSpace(v), " ", "") }

// ValidIIN проверяет 12 цифр и контрольную сумму ИИН Казахстана.
func ValidIIN(v string) bool {
	v = NormalizeIIN(v)
	if len(v) != 12 {
		return false
	}
	d := make([]int, 12)
	for i := range v {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
		d[i] = int(v[i] - '0')
	}
	check := 0
	for i := 0; i < 11; i++ {
		check += d[i] * (i + 1)
	}
	check %= 11
	if check == 10 {
		check = 0
		weights := [...]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 1, 2}
		for i, w := range weights {
			check += d[i] * w
		}
		check %= 11
	}
	return check != 10 && check == d[11]
}
