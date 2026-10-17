package domain

import (
	"encoding/base64"
	"math"
	"strings"
)

// DistanceM — расстояние между точками по гаверсинусу, в метрах.
func DistanceM(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Sqrt(h))
}

const signaturePrefix = "data:image/png;base64,"

// ValidSignature — подпись пальцем приходит как PNG data URL; проверяем формат и разумный размер.
func ValidSignature(s string) bool {
	if !strings.HasPrefix(s, signaturePrefix) || len(s) < 200 || len(s) > 400_000 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(s[len(signaturePrefix):])
	return err == nil && strings.HasPrefix(string(raw), "\x89PNG\r\n\x1a\n")
}
