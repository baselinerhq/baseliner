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
