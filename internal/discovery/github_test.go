package discovery

import "testing"

func TestIncludeExcludeGlobs(t *testing.T) {
	d := GitHub{Include: []string{"svc-*", "lib-*"}, Exclude: []string{"*-old", "archived-*"}}

	cases := []struct {
		name             string
		excluded, includ bool
	}{
		{"svc-api", false, true},
		{"lib-core", false, true},
		{"docs", false, false},      // not in include
		{"svc-api-old", true, true}, // excluded wins at the call site
		{"archived-thing", true, false},
	}
	for _, c := range cases {
		if got := d.isExcluded(c.name); got != c.excluded {
			t.Errorf("isExcluded(%q) = %v, want %v", c.name, got, c.excluded)
		}
		if got := d.isIncluded(c.name); got != c.includ {
			t.Errorf("isIncluded(%q) = %v, want %v", c.name, got, c.includ)
		}
	}
}

func TestEmptyIncludeMatchesAll(t *testing.T) {
	d := GitHub{} // no include/exclude
	if !d.isIncluded("anything") {
		t.Error("empty include should match all")
	}
	if d.isExcluded("anything") {
		t.Error("empty exclude should match none")
	}
}
