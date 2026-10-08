package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

// A forge other than GitHub sets visibility at discovery. A source without
// one is protected, not public.
func TestRepoVisibilityForgeSource(t *testing.T) {
	vis := repoVisibility([]source.Repo{
		{Type: "forgex", Slug: "g/inside", Visibility: "internal"},
		{Type: "forgex", Slug: "g/unknown"},
		{Type: "forgex", Slug: "g/open", Visibility: "public"},
		{Type: "local", Slug: "/tmp/x"},
	})
	want := map[string]string{"g/inside": "internal", "g/unknown": "private", "g/open": "public"}
	if fmt.Sprint(vis) != fmt.Sprint(want) {
		t.Errorf("repoVisibility = %v, want %v", vis, want)
	}
}

// Every alias of a protected source is redacted, as its slug is; a public
// source's aliases stay readable.
func TestGuardStderrRedactsAliases(t *testing.T) {
	sources := []source.Repo{
		{Type: "forgex", Slug: "g/team/secret", Visibility: "private", Aliases: []string{"G/Team/Secret", "g%2Fteam%2Fsecret"}},
		{Type: "forgex", Slug: "g/open-kit", Visibility: "public", Aliases: []string{"g%2Fopen-kit"}},
	}
	var logs, errb bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	public := true
	cfg := &config.Config{Privacy: &config.PrivacyConfig{PublicContext: &public}}
	_, restore := guardStderr(&errb, sources, cfg, Options{})
	slog.Warn("read failed", "err", errors.New("GET /projects/g%2Fteam%2Fsecret/repository/tree: 500"))
	slog.Warn("read failed", "err", errors.New("GET /projects/g%2Fopen-kit/repository/tree: 500"))
	restore()
	if strings.Contains(strings.ToLower(logs.String()), "secret") {
		t.Errorf("an alias of the private repo was not redacted:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "g%2Fopen-kit") {
		t.Errorf("a public alias was redacted:\n%s", logs.String())
	}
}

type stubCollector struct{ name string }

func (s stubCollector) Collect(_ context.Context, src source.Repo) *models.NormalizedRepository {
	return &models.NormalizedRepository{Slug: src.Slug, Name: s.name}
}

// Each source goes to the collector for its type, and a local source to the
// filesystem and git collectors; the output keeps the sources' order.
func TestCollectAllDispatchesByType(t *testing.T) {
	dir := t.TempDir()
	sources := []source.Repo{
		{Type: "forgex", Slug: "x/1"},
		{Type: "local", Slug: dir, Path: dir},
		{Type: "forgey", Slug: "y/1"},
		{Type: "forgex", Slug: "x/2"},
	}
	cols := map[string]repoCollector{"forgex": stubCollector{"X"}, "forgey": stubCollector{"Y"}}
	repos, errs := collectAll(context.Background(), sources, cols, time.Now())
	if len(errs) != 0 || len(repos) != 4 {
		t.Fatalf("repos %d, errors %v", len(repos), errs)
	}
	var got []string
	for _, r := range repos {
		got = append(got, r.Slug+"="+r.Name+":"+string(r.SourceType))
	}
	want := []string{"x/1=X:", dir + "=" + repos[1].Name + ":" + string(models.SourceLocal), "y/1=Y:", "x/2=X:"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("collected %v, want %v", got, want)
	}
}

type stubRateLimit struct{ reset time.Time }

func (stubRateLimit) Error() string                    { return "429" }
func (e stubRateLimit) RateLimit() (string, time.Time) { return "Forgex", e.reset }

// Another forge's rate-limit refusals are counted under its own name, with the
// latest reset it gave.
func TestRateLimitWatchForgeError(t *testing.T) {
	var w rateLimitWatch
	early := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	w.observe(fmt.Errorf("listing: %w", stubRateLimit{early}))
	w.observe(stubRateLimit{early.Add(time.Hour)})
	w.observe(errors.New("not a refusal"))
	got := w.summary()
	if !strings.HasPrefix(got, "Forgex refused 2 request(s) under its API rate limit") ||
		!strings.HasSuffix(got, "; the limit resets at 2026-10-08 13:00 UTC") || strings.Contains(got, "\n") {
		t.Errorf("summary = %q", got)
	}
}
