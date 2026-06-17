package models

import (
	"encoding/json"
	"testing"
)

// Score must serialize integral values with a decimal point (1.0, 0.0) to match
// pydantic, while non-integral values use the shortest round-trip form.
func TestScoreMarshalJSON(t *testing.T) {
	cases := map[Score]string{
		1.0:    "1.0",
		0.0:    "0.0",
		0.5:    "0.5",
		0.6087: "0.6087",
		0.0001: "0.0001",
	}
	for in, want := range cases {
		got, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal %v: %v", float64(in), err)
		}
		if string(got) != want {
			t.Errorf("Score(%v) = %s, want %s", float64(in), got, want)
		}
	}
}
