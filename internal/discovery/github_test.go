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

// globMatch must follow fnmatch semantics, not Go's path.Match. These cases are
// exactly where the two engines disagree (verified against Python's fnmatch).
func TestGlobMatchFnmatchSemantics(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		// Negated character class uses `!`, not `^`.
		{"[!abc]", "a", false},
		{"[!abc]", "x", true},
		{"svc-[!0-9]", "svc-x", true},
		{"svc-[!0-9]", "svc-5", false},
		// A bare `^` inside a class is literal in fnmatch (not negation).
		{"[^abc]", "^", true},
		{"[^abc]", "x", false},
		// Malformed (unterminated) class is treated literally and can still match.
		{"svc-[abc", "svc-[abc", true},
		{"svc-[abc", "svc-a", false},
		// Ordinary wildcards behave as expected.
		{"svc-*", "svc-api", true},
		{"*-old", "thing-old", true},
		{"lib-?", "lib-x", true},
		{"lib-?", "lib-xy", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
