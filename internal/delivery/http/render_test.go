package httpdelivery

import "testing"

func TestMoney(t *testing.T) {
	for n, want := range map[int]string{0: "0", 990: "990", 9990: "9 990", 1234567: "1 234 567", -9990: "-9 990"} {
		if got := money(n); got != want {
			t.Errorf("money(%d) = %q, want %q", n, got, want)
		}
	}
}
