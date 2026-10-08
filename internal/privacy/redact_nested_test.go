package privacy

import "testing"

// GitLab paths nest: group/sub/project. A private slug inside a whole public
// name of any depth is left alone, since masking it would leave the rest of
// the public name beside "private/redacted", from which the private name can
// be inferred. Anywhere else it is masked.
func TestRedactorSparesNestedPublicNames(t *testing.T) {
	r := NewRedactor(map[string]string{"acme/app": "private", "acme/app-tools/cli": "public"}, active)
	for in, want := range map[string]string{
		"acme/app-tools/cli":                  "acme/app-tools/cli",
		"ACME/App-Tools/CLI.git":              "ACME/App-Tools/CLI.git",
		"see acme/app-tools/cli.":             "see acme/app-tools/cli.",
		"GET /x/acme/app-tools/cli/tree: 500": "GET /x/acme/app-tools/cli/tree: 500",
		"acme/app-tools/cli/acme/app":         "acme/app-tools/cli/private/redacted",
		// Not a known public name: over-redacted, never left readable.
		"acme/app-tools":       "private/redacted-tools",
		"acme/app-tools/cli-x": "private/redacted-tools/cli-x",
		"acme/app ok":          "private/redacted ok",
		"acme/app/sub":         "private/redacted/sub",
	} {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

// A spared match is only safe while the public name around it survives. A
// protected slug can cross that name's edge at any of its slashes, not only
// its first, and its redaction would cut into the name.
func TestRedactorCutAtLaterSlash(t *testing.T) {
	for _, c := range []struct {
		vis      map[string]string
		in, want string
	}{
		{map[string]string{"a/b": "private", "c/open-kit/z": "private", "a/b/c/open-kit": "public"},
			"a/b/c/open-kit/z", "private/redacted/private/redacted"},
		{map[string]string{"g/app": "private", "x/y/g": "private", "g/app/cli": "public"},
			// Overlapping matches are masked as one: masking "x/y/g" alone
			// would leave "/app" of "g/app" readable.
			"x/y/g/app/cli", "private/redacted/cli"},
	} {
		if got := NewRedactor(c.vis, active).String(c.in); got != c.want {
			t.Errorf("String(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// GitLab API URLs can carry a project path URL-encoded. Those spellings are
// protected like any other alias, in either case of the escape.
func TestRedactorEncodedAliases(t *testing.T) {
	r := NewRedactor(map[string]string{
		"acme/team/app": "private", "acme%2Fteam%2Fapp": "private",
		"acme/team/app-kit": "public", "acme%2Fteam%2Fapp-kit": "public",
	}, active)
	for in, want := range map[string]string{
		"GET /projects/acme%2Fteam%2Fapp/repository/tree: 500": "GET /projects/private/redacted/repository/tree: 500",
		"GET /projects/acme%2fteam%2fapp/repository/tree: 500": "GET /projects/private/redacted/repository/tree: 500",
		"acme%2Fteam%2Fapp-kit":                                "acme%2Fteam%2Fapp-kit",
		"acme%2Fteam%2Fapp-x":                                  "private/redacted-x",
	} {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two protected slugs can overlap on GitHub too, when one owner is named like
// another repo. Masking only the first match would leave the rest of the
// second readable ("private/redacted/x").
func TestRedactorMasksOverlappingSlugs(t *testing.T) {
	r := NewRedactor(map[string]string{"acme/open": "private", "open/x": "private"}, active)
	for in, want := range map[string]string{
		"GET /repos/acme/open/x/branches": "GET /repos/private/redacted/branches",
		"acme/open open/x":                "private/redacted private/redacted",
	} {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

// The nested counterpart of FuzzRedactorNeverLeaksPrivateSlug, with private
// and public names of several depths that contain one another.
func FuzzRedactorNeverLeaksNestedPrivateSlug(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"GET /projects/", "/repository/tree"}, {"", "-tools/cli"},
		{"x/", ""}, {"team/", "/x"}, {"acme/", "-kit/cli"}, {"", ".git"}, {"acme/team/", ""}} {
		f.Add(seed[0], seed[1])
	}
	private := []string{"acme/team/app", "acme/app", "team/app/x"}
	public := []string{"acme/team/app-kit/cli", "acme/app-tools/cli", "x/acme/app"}
	vis := map[string]string{}
	for _, p := range private {
		vis[p] = "private"
	}
	for _, p := range public {
		vis[p] = "public"
	}
	r := NewRedactor(vis, active)
	f.Fuzz(func(t *testing.T, prefix, suffix string) {
		in := prefix + "acme/app" + suffix
		if out := r.String(in); readablePrivate(out, private, public) {
			t.Errorf("String(%q) = %q leaves a private slug readable", in, out)
		}
	})
}

// The public name around a match can begin segments before it, must itself be
// a whole name, and is cut by a protected slug crossing its end even when a
// ".git" follows.
func TestRedactorNestedEdges(t *testing.T) {
	for _, c := range []struct {
		vis      map[string]string
		in, want string
	}{
		// Begins before the match's own segment.
		{map[string]string{"team/app": "private", "acme/team/app-kit": "public"},
			"GET /x/acme/team/app-kit: 500", "GET /x/acme/team/app-kit: 500"},
		// A known public name inside a longer unknown one is not a whole name.
		{map[string]string{"acme/app": "private", "p/acme/app-x": "public"},
			"zp/acme/app-x", "zp/private/redacted-x"},
		// A protected slug crossing the public name's end, before its ".git".
		{map[string]string{"g/x": "private", "a/kit.g": "private", "g/x/a/kit": "public"},
			"g/x/a/kit.git", "private/redacted/private/redactedit"},
	} {
		if got := NewRedactor(c.vis, active).String(c.in); got != c.want {
			t.Errorf("String(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
