package privacy

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var testVis = map[string]string{
	"o/pub":   "public",
	"o/app":   "private",
	"o/app-x": "internal",
}

var active = Options{PublicContext: true, Mode: ModeRedact}

func TestNewRedactorInactive(t *testing.T) {
	cases := map[string]struct {
		vis map[string]string
		o   Options
	}{
		"not public":      {testVis, Options{PublicContext: false, Mode: ModeRedact}},
		"allow":           {testVis, Options{PublicContext: true, Mode: ModeAllow}},
		"nothing to hide": {map[string]string{"o/pub": "public"}, active},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if r := NewRedactor(c.vis, c.o); r != nil {
				t.Fatalf("want nil redactor, got %+v", r)
			}
		})
	}
}

// Exclude and fail protect private repos too, so their names must not reach
// stderr either.
func TestNewRedactorActiveInEveryProtectingMode(t *testing.T) {
	for _, m := range []Mode{ModeRedact, ModeExclude, ModeFail} {
		if NewRedactor(testVis, Options{PublicContext: true, Mode: m}) == nil {
			t.Errorf("mode %s: want a redactor, got nil", m)
		}
	}
}

func TestRedactorString(t *testing.T) {
	r := NewRedactor(testVis, active)
	in := "POST https://api.github.com/repos/o/app/labels: 403; o/app-x failed; o/pub ok"
	// "o/app-x" must be masked whole: matching the shorter "o/app" inside it
	// would leave "private/redacted-x", a fragment of the private name.
	want := "POST https://api.github.com/repos/private/redacted/labels: 403; private/redacted failed; o/pub ok"
	if got := r.String(in); got != want {
		t.Errorf("String =\n  %q\nwant\n  %q", got, want)
	}
}

// GitHub owner and repo names are case-insensitive, and the slug is spelled as
// the config spells the owner while API URLs use GitHub's own spelling. A
// case-sensitive match left "repos/acme/app" in the log when the config said
// "Acme".
func TestRedactorStringIgnoresCase(t *testing.T) {
	r := NewRedactor(map[string]string{"Acme/App": "private", "acme/pub": "public"}, active)
	in := "GET https://api.github.com/repos/acme/app/readme: 403; ACME/APP; Acme/App; acme/pub ok"
	want := "GET https://api.github.com/repos/private/redacted/readme: 403; private/redacted; private/redacted; acme/pub ok"
	if got := r.String(in); got != want {
		t.Errorf("String =\n  %q\nwant\n  %q", got, want)
	}
}

// A '.' in a repo name is literal: "o/a.b" must not mask the public "o/axb".
func TestRedactorStringMatchesNamesLiterally(t *testing.T) {
	r := NewRedactor(map[string]string{"o/a.b": "private", "o/axb": "public"}, active)
	if got, want := r.String("o/a.b o/axb"), "private/redacted o/axb"; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
}

// Exclude mode promises a private repo is absent, not masked: a log record
// that mentions one is dropped, wherever the slug sits, while records about
// public repos still reach the log.
func TestRedactorHandlerDropsInExcludeMode(t *testing.T) {
	var buf bytes.Buffer
	r := NewRedactor(testVis, Options{PublicContext: true, Mode: ModeExclude})
	log := slog.New(r.Handler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	log.Info("created issue for o/app")
	log.Info("created issue", "repo", "O/App", "number", 5)
	log.Warn("could not create label", "err", errors.New("POST .../repos/o/app-x/labels: 403"))
	log.With("repo", "o/app").Warn("bound attr")
	log.WithGroup("g").Info("grouped", slog.Group("inner", "repo", "o/app"))
	log.With("repo", "o/app").WithGroup("g").Info("bound, then grouped")
	log.Info("public", "repo", "o/pub")

	out := buf.String()
	if strings.Contains(strings.ToLower(out), "o/app") || strings.Contains(out, RedactedSlug) {
		t.Errorf("exclude mode must drop records about private repos, not mask them:\n%s", out)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "repo=o/pub") {
		t.Errorf("want only the public record:\n%s", out)
	}
}

func TestNilRedactorIsNoOp(t *testing.T) {
	var r *Redactor
	if got := r.String("o/app"); got != "o/app" {
		t.Errorf("String = %q", got)
	}
	var buf bytes.Buffer
	if r.Writer(&buf) != &buf {
		t.Error("Writer should return w unchanged")
	}
	h := slog.NewTextHandler(&buf, nil)
	if r.Handler(h) != slog.Handler(h) {
		t.Error("Handler should return h unchanged")
	}
}

func TestRedactorWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewRedactor(testVis, active).Writer(&buf)
	in := []byte("1 repo(s) below --fail-under 0.90: o/app (0.50)\n")
	n, err := w.Write(in)
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	if strings.Contains(buf.String(), "o/app") {
		t.Errorf("slug leaked: %q", buf.String())
	}
}

func TestRedactorHandler(t *testing.T) {
	var buf bytes.Buffer
	r := NewRedactor(testVis, active)
	log := slog.New(r.Handler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	log.Info("created issue for o/app", "repo", "o/app", "number", 5)
	log.Warn("could not create label", "err", errors.New("POST .../repos/o/app-x/labels: 403"))
	log.With("repo", "o/app").Warn("bound attr")
	log.WithGroup("g").Info("grouped", slog.Group("inner", "repo", "o/app"))
	log.Info("public", "repo", "o/pub")

	out := buf.String()
	if strings.Contains(out, "o/app") {
		t.Errorf("private slug leaked:\n%s", out)
	}
	if !strings.Contains(out, "repo=o/pub") {
		t.Errorf("public slug should be untouched:\n%s", out)
	}
	if !strings.Contains(out, "number=5") {
		t.Errorf("non-string attrs should pass through:\n%s", out)
	}
}

// A private slug inside one of the scan's public names is left alone: masking
// "acme/open" inside the public "acme/open-kit" would show it as
// "private/redacted-kit", from which the private name can be inferred. Every
// other occurrence is masked, however it is punctuated, prefixed or suffixed.
func TestRedactorSparesOnlyKnownPublicNames(t *testing.T) {
	r := NewRedactor(map[string]string{"acme/open": "private", "acme/open-kit": "public", "acme/open.js": "public", "bigacme/open": "public"}, active)
	for in, want := range map[string]string{
		// Known public names stay intact.
		"acme/open-kit acme/open.js bigacme/open": "acme/open-kit acme/open.js bigacme/open",
		"ACME/Open-Kit.git":                       "ACME/Open-Kit.git",
		"see acme/open-kit.":                      "see acme/open-kit.",
		"see acme/open-kit...":                    "see acme/open-kit...",
		"acme/open-kit/acme/open":                 "acme/open-kit/private/redacted",
		// The private slug, however it appears.
		"acme/open":                               "private/redacted",
		"failed for acme/open.":                   "failed for private/redacted.",
		"cloning acme/open...":                    "cloning private/redacted...",
		"git@github.com:acme/open.git":            "git@github.com:private/redacted.git",
		"clone https://github.com/acme/open.git.": "clone https://github.com/private/redacted.git.",
		"GET https://api.github.com/repos/acme/open/branches?per_page=100": "GET https://api.github.com/repos/private/redacted/branches?per_page=100",
		"acme/open acme/open,ACME/OPEN;acme/open":                          "private/redacted private/redacted,private/redacted;private/redacted",
		"(acme/open)`acme/open`\"acme/open\"":                              "(private/redacted)`private/redacted`\"private/redacted\"",
		// Unknown names around it are over-redacted rather than left readable.
		"wiki: acme/open.wiki":                                     "wiki: private/redacted.wiki",
		"acme/open-related failure":                                "private/redacted-related failure",
		"\x1b[31macme/open\x1b[0m":                                 "\x1b[31mprivate/redacted\x1b[0m",
		`{"msg":"line1\nacme/open"}`:                               `{"msg":"line1\nprivate/redacted"}`,
		"q=repo%3Aacme/open":                                       "q=repo%3Aprivate/redacted",
		"_acme/open .acme/open 1acme/open acme/open_ acme/open.v2": "_private/redacted .private/redacted 1private/redacted private/redacted_ private/redacted.v2",
	} {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
}

// Whatever surrounds a private slug, once the scan's public names are taken
// out of the output, the private slug must not be left. The oracle does not
// share the redactor's rules, so it can catch one that is wrong.
func FuzzRedactorNeverLeaksPrivateSlug(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"GET /repos/", "/branches"}, {"x", ".git"}, {"(", ")."}, {"", "-kit"}, {"big", ""}, {"\x1b[31m", "..."}} {
		f.Add(seed[0], seed[1])
	}
	public := []string{"acme/open-kit", "bigacme/open"}
	r := NewRedactor(map[string]string{"acme/open": "private", public[0]: "public", public[1]: "public"}, active)
	f.Fuzz(func(t *testing.T, prefix, suffix string) {
		in := prefix + "acme/open" + suffix
		left := strings.ToLower(r.String(in))
		for _, p := range public {
			left = strings.ReplaceAll(left, p, "")
		}
		if strings.Contains(left, "acme/open") {
			t.Errorf("String(%q) = %q leaves the private slug readable", in, r.String(in))
		}
	})
}
