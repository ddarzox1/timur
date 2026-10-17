package domain

import "time"

// KZ — с 2024 года весь Казахстан живёт в UTC+5.
var KZ = time.FixedZone("KZ", 5*3600)

// Now подменяется в тестах.
var Now = time.Now

// Today — текущая дата в Казахстане (полночь, UTC-значение даты).
func Today() time.Time {
	n := Now().In(KZ)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}
