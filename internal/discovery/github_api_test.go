package discovery

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/config"
)

func fakeClient(t *testing.T, h http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := github.NewClient(nil)
	u, _ := url.Parse(srv.URL + "/")
	c.BaseURL = u
	return c
}

const rateOK = `{"resources":{"core":{"limit":5000,"remaining":5000,"reset":0}}}`

func TestGitHubDiscoverOrgWithFilters(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rateOK)) })
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"svc-api"},{"name":"docs"},{"name":"svc-old"}]`))
	})

	d := GitHub{
		Client:  fakeClient(t, mux),
		Cfg:     config.GitHubScope{Type: "org", Name: "acme"},
		Include: []string{"svc-*"},
		Exclude: []string{"*-old"},
	}
	got, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// svc-api: included; docs: not in include; svc-old: excluded.
	if len(got) != 1 || got[0].Slug != "acme/svc-api" || got[0].Type != "github" {
		t.Fatalf("got %+v, want one source acme/svc-api", got)
	}
	if got[0].GitHubRepo == nil {
		t.Error("source should carry the *github.Repository")
	}
}

// Discovery logs repos it filters out at debug level. A private repo is often
// excluded precisely to keep it out of a public report, and these lines run
// before any redaction knows about it (it is never scanned), so its name must
// not be logged at all.
func TestGitHubDiscoverDoesNotLogPrivateNames(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rateOK)) })
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"svc-api"},{"name":"secret-lab","private":true},` +
			`{"name":"inner-tool","visibility":"internal"},{"name":"public-old"},` +
			`{"name":"shelved-secret","private":true,"archived":true}]`))
	})

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	d := GitHub{
		Client:  fakeClient(t, mux),
		Cfg:     config.GitHubScope{Type: "org", Name: "acme"},
		Include: []string{"svc-*", "secret-*", "public-*"},
		Exclude: []string{"secret-*", "*-old"},
	}
	if _, err := d.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out := logs.String()
	for _, name := range []string{"secret-lab", "inner-tool", "shelved-secret"} {
		if strings.Contains(out, name) {
			t.Errorf("private repo %q named in debug log:\n%s", name, out)
		}
	}
	// A public repo's name stays, so the check can see a name when one is there.
	if !strings.Contains(out, "public-old") {
		t.Errorf("public repo name missing from debug log:\n%s", out)
	}
}

// An archived repo is read-only: once it ages past stale_repo's threshold it
// fails the gate permanently and can't be fixed, so discovery skips it unless
// include_archived opts back in.
func TestGitHubDiscoverArchived(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rateOK)) })
	mux.HandleFunc("GET /orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"live"},{"name":"retired","archived":true}]`))
	})
	client := fakeClient(t, mux)

	for _, c := range []struct {
		name    string
		include bool
		want    []string
	}{
		{"skipped by default", false, []string{"acme/live"}},
		{"included on opt-in", true, []string{"acme/live", "acme/retired"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := GitHub{Client: client, Cfg: config.GitHubScope{Type: "org", Name: "acme", IncludeArchived: c.include}}
			got, err := d.Discover(context.Background())
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			var slugs []string
			for _, s := range got {
				slugs = append(slugs, s.Slug)
			}
			if strings.Join(slugs, ",") != strings.Join(c.want, ",") {
				t.Errorf("got %v, want %v", slugs, c.want)
			}
		})
	}
}

func TestGitHubDiscoverUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rateOK)) })
	mux.HandleFunc("GET /users/octo/repos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"tool"},{"name":"lib"}]`))
	})

	d := GitHub{Client: fakeClient(t, mux), Cfg: config.GitHubScope{Type: "user", Name: "octo"}}
	got, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2", len(got))
	}
}

func TestGitHubDiscoverRateLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rate_limit", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":0,"reset":0}}}`))
	})

	d := GitHub{Client: fakeClient(t, mux), Cfg: config.GitHubScope{Type: "org", Name: "acme"}}
	_, err := d.Discover(context.Background())
	if err == nil {
		t.Fatal("expected a rate-limit error when remaining is 0")
	}
	var rl *config.RateLimitError
	if !errors.As(err, &rl) {
		t.Errorf("expected *config.RateLimitError, got %T", err)
	}
}
