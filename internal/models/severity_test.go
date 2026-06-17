package models

import "testing"

func TestSeverityWeight(t *testing.T) {
	cases := []struct {
		sev  Severity
		want int
	}{
		{SeverityCritical, 4},
		{SeverityHigh, 3},
		{SeverityMedium, 2},
		{SeverityLow, 1},
		{Severity("unknown"), 1}, // fallback
	}
	for _, c := range cases {
		if got := c.sev.Weight(); got != c.want {
			t.Errorf("%q.Weight() = %d, want %d", c.sev, got, c.want)
		}
	}
}
