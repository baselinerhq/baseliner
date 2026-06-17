package discovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
