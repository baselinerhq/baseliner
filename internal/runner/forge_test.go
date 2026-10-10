package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/collectors"
	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/gitlab"
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
	repos, errs := collectAll(context.Background(), sources, cols, nil, time.Now())
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

// A spelling shared by a protected and a public source stays protected,
// whichever comes first: an alias spelled like another source's slug, or the
// same slug on two forges.
func TestVisibilityProtectedSpellingWins(t *testing.T) {
	priv := &github.Repository{Name: github.Ptr("priv"), FullName: github.Ptr("o/priv"),
		Owner: &github.User{Login: github.Ptr("o")}, Visibility: github.Ptr("private")}
	for name, sources := range map[string][]source.Repo{
		"alias after slug": {
			{Type: "forgex", Slug: "g/secret", Visibility: "private"},
			{Type: "forgex", Slug: "g/open", Visibility: "public", Aliases: []string{"g/secret"}},
		},
		"alias before slug": {
			{Type: "forgex", Slug: "g/open", Visibility: "public", Aliases: []string{"g/secret"}},
			{Type: "forgex", Slug: "g/secret", Visibility: "private"},
		},
		"alias across forges": {
			{Type: "github", Slug: "o/priv", GitHubRepo: priv},
			{Type: "forgex", Slug: "g/open", Visibility: "public", Aliases: []string{"o/priv"}},
		},
		"mixed-case public first": {
			{Type: "forgex", Slug: "g/secret", Visibility: "Public"},
			{Type: "forgey", Slug: "g/secret", Visibility: "private"},
		},
		"same slug on two forges": {
			{Type: "forgex", Slug: "o/priv", Visibility: "private"},
			{Type: "github", Slug: "o/priv", GitHubRepo: &github.Repository{Name: github.Ptr("priv"), Visibility: github.Ptr("public")}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			vis := withAliases(repoVisibility(sources), sources)
			for _, n := range []string{"g/secret", "o/priv"} {
				if v, ok := vis[n]; ok && v == "public" {
					t.Errorf("%s = public: %v", n, vis)
				}
			}
		})
	}
}

// A GitHub source without its API record stays out of the visibility map, as
// before forge sources existed.
func TestRepoVisibilityGitHubWithoutRecord(t *testing.T) {
	if vis := repoVisibility([]source.Repo{{Type: "github", Slug: "o/x"}}); len(vis) != 0 {
		t.Errorf("repoVisibility = %v, want empty", vis)
	}
}

// A forge source with no collector is a collection error, not an empty local
// directory whose every file is missing.
func TestCollectAllUnknownForgeIsAnError(t *testing.T) {
	repos, errs := collectAll(context.Background(), []source.Repo{{Type: "forgez", Slug: "z/1"}}, map[string]repoCollector{}, nil, time.Now())
	if len(repos) != 0 || len(errs) != 1 || errs[0].Slug != "z/1" || errs[0].Forge != "forgez" {
		t.Fatalf("repos %v, errors %v", repos, errs)
	}
}

type panicCollector struct{}

func (panicCollector) Collect(context.Context, source.Repo) *models.NormalizedRepository {
	panic("boom")
}

// A collector's panic becomes a collection error for that repo, which keeps
// its forge.
func TestCollectAllPanicKeepsForge(t *testing.T) {
	_, errs := collectAll(context.Background(), []source.Repo{{Type: "gitlab", Slug: "g/1"}},
		map[string]repoCollector{"gitlab": panicCollector{}}, nil, time.Now())
	if len(errs) != 1 || errs[0].Slug != "g/1" || errs[0].Forge != "gitlab" {
		t.Fatalf("errors %v", errs)
	}
}

type namedRateLimit struct {
	forge string
	reset time.Time
}

func (namedRateLimit) Error() string                    { return "429" }
func (e namedRateLimit) RateLimit() (string, time.Time) { return e.forge, e.reset }

// Each forge gets its own line, in name order, with its own latest reset.
func TestRateLimitWatchTwoForges(t *testing.T) {
	var w rateLimitWatch
	at := func(h int) time.Time { return time.Date(2026, 10, 8, h, 0, 0, 0, time.UTC) }
	w.observe(namedRateLimit{"Zforge", at(15)})
	w.observe(namedRateLimit{"Aforge", at(14)})
	w.observe(namedRateLimit{"Aforge", at(13)}) // earlier than the one already seen
	lines := strings.Split(w.summary(), "\n")
	if len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "Aforge refused 2 request(s)") || !strings.HasSuffix(lines[0], "2026-10-08 14:00 UTC") ||
		!strings.HasPrefix(lines[1], "Zforge refused 1 request(s)") || !strings.HasSuffix(lines[1], "2026-10-08 15:00 UTC") {
		t.Errorf("summary =\n%s", w.summary())
	}
}

// A forge's visibility is compared and stored without regard to case: "Public"
// recorded first must not hold off a private value, and "Internal" is stored
// as the privacy guard and ignore_when expect it.
func TestVisibilityCase(t *testing.T) {
	vis := map[string]string{"g/x": "Public"}
	setVisibility(vis, "g/x", "private")
	if vis["g/x"] != "private" {
		t.Errorf("setVisibility kept %q over private", vis["g/x"])
	}
	if v := repoVisibility([]source.Repo{{Type: "forgex", Slug: "g/y", Visibility: " Internal "}})["g/y"]; v != "internal" {
		t.Errorf("visibility = %q, want internal", v)
	}
}

// Every forge's collector, and the local walk, gets the policy's extra
// directories.
func TestCollectorsGetExtraDirs(t *testing.T) {
	gl, err := gitlab.New("http://127.0.0.1:1", "t")
	if err != nil {
		t.Fatal(err)
	}
	gt, err := gitea.New("http://127.0.0.1:1", "t")
	if err != nil {
		t.Fatal(err)
	}
	extra := []string{"config"}
	cols := forgeClients{github: github.NewClient(nil), gitlab: gl, gitea: gt}.collectors(false, extra, nil)
	got := map[string][]string{
		"github": cols["github"].(collectors.GitHubAPI).ExtraDirs,
		"gitlab": cols["gitlab"].(collectors.GitLabAPI).ExtraDirs,
		"gitea":  cols["gitea"].(collectors.GiteaAPI).ExtraDirs,
	}
	for forge, dirs := range got {
		if !slices.Equal(dirs, extra) {
			t.Errorf("%s ExtraDirs = %v", forge, dirs)
		}
	}

	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ops", "ci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "ops"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "ops"), 0o755) })
	repos, _ := collectAll(context.Background(), []source.Repo{{Type: "local", Slug: root, Path: root}}, nil, []string{"ops/ci"}, time.Now())
	if len(repos) != 1 || !slices.Contains(repos[0].FS.PolicyUnreadDirs, "ops/ci") {
		t.Errorf("local walk: policy unread %v lacks ops/ci", repos[0].FS.PolicyUnreadDirs)
	}
}
