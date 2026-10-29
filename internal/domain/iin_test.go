package domain

import "testing"

func TestValidIIN(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"870514300470", true},
		{" 870514300470 ", true},
		{"870514300478", false},
		{"123", false},
		{"87051430047x", false},
	} {
		if got := ValidIIN(tc.value); got != tc.want {
			t.Errorf("ValidIIN(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
