package domain

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"+7 (701) 000-00-01": "77010000001",
		"87010000001":        "77010000001",
		"7010000001":         "77010000001",
		"77010000001":        "77010000001",
		"+1 555 0000":        "",
		"":                   "",
		"8 (727) 1":          "",
	} {
		if got := NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDistanceAndContains(t *testing.T) {
	// 0.001° широты ≈ 111 м
	if d := DistanceM(43.2389, 76.8897, 43.2399, 76.8897); d < 105 || d > 116 {
		t.Fatalf("DistanceM = %v", d)
	}
	lat, lng := 43.2389, 76.8897
	l := Location{Lat: &lat, Lng: &lng, RadiusM: 300}
	if in, ok := l.Contains(43.2390, 76.8899); !in || !ok {
		t.Fatal("точка рядом должна быть внутри")
	}
	if in, ok := l.Contains(43.30, 76.95); in || !ok {
		t.Fatal("точка в 8 км должна быть снаружи")
	}
	if _, ok := (Location{}).Contains(1, 1); ok {
		t.Fatal("без координат объекта ok=false")
	}
}

func pngURL(extra int) string {
	raw := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", extra)
	return signaturePrefix + base64.StdEncoding.EncodeToString([]byte(raw))
}

func TestValidSignature(t *testing.T) {
	cases := map[string]bool{
		pngURL(300):                      true,
		pngURL(10):                       false, // слишком короткая — пустой холст
		"data:image/jpeg;base64,AAAA":    false,
		signaturePrefix + "!!!не base64": false,
		signaturePrefix + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("GIF89a", 60))): false,
	}
	for in, want := range cases {
		if got := ValidSignature(in); got != want {
			t.Errorf("ValidSignature(%.40q…) = %v, want %v", in, got, want)
		}
	}
}

func withToday(t *testing.T, y int, m time.Month, d int) {
	old := Now
	Now = func() time.Time { return time.Date(y, m, d, 12, 0, 0, 0, KZ) }
	t.Cleanup(func() { Now = old })
}

func TestHazardRules(t *testing.T) {
	withToday(t, 2026, 10, 16)
	yesterday := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	h := Hazard{OrgID: 1, Status: "open", DueAt: &yesterday}
	if !h.Overdue() {
		t.Fatal("открытая с прошедшим сроком — просрочена")
	}
	h.Status = "fixed"
	if h.Overdue() {
		t.Fatal("устранённая не просрочена")
	}

	author, resp := int64(10), int64(20)
	h = Hazard{OrgID: 1, AuthorID: &author, ResponsibleID: &resp}
	for _, c := range []struct {
		u    User
		want bool
	}{
		{User{ID: 10, OrgID: 1, Role: RoleWorker}, true},  // автор
		{User{ID: 20, OrgID: 1, Role: RoleWorker}, true},  // ответственный
		{User{ID: 30, OrgID: 1, Role: RoleWorker}, false}, // посторонний сотрудник
		{User{ID: 30, OrgID: 1, Role: RoleHSE}, true},     // инженер ОТ
		{User{ID: 30, OrgID: 2, Role: RoleAdmin}, false},  // админ чужой организации
	} {
		if got := h.VisibleTo(c.u); got != c.want {
			t.Errorf("VisibleTo(%+v) = %v", c.u, got)
		}
	}
}

func TestDaysWithoutInjury(t *testing.T) {
	today := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	o := Org{CreatedAt: time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC)}
	if d := o.DaysWithoutInjury(today); d != 128 {
		t.Fatalf("от подключения: %d", d)
	}
	inj := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	o.LastInjuryAt = &inj
	if d := o.DaysWithoutInjury(today); d != 10 {
		t.Fatalf("от травмы: %d", d)
	}
}

func TestAssignmentOverdue(t *testing.T) {
	withToday(t, 2026, 10, 16)
	a := Assignment{DueAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	if !a.Overdue() {
		t.Fatal("неподписанный с прошедшим сроком — просрочен")
	}
	now := time.Now()
	a.SignedAt = &now
	if a.Overdue() {
		t.Fatal("подписанный не просрочен")
	}
	if (Assignment{Title: "RU", TitleKK: "KK"}).TitleIn("kk") != "KK" || (Assignment{Title: "RU"}).TitleIn("kk") != "RU" {
		t.Fatal("TitleIn")
	}
}

func TestClip(t *testing.T) {
	if Clip("  Қауіп  ", 10) != "Қауіп" || Clip("абвгд", 3) != "абв" || len([]rune(Clip(strings.Repeat("ы", 3000), MaxText))) != MaxText {
		t.Fatal("Clip режет по символам")
	}
}
