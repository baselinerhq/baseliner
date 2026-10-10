package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/gitea"
)

// fakeGiteaOrg serves org "acme" and user "octo" listings in the shapes
// Forgejo 16 returns, or status for every request when non-zero.
func fakeGiteaOrg(t *testing.T, status int) *gitea.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"detail naming acme/secret-lab"}`))
			return
		}
		switch strings.ToLower(r.URL.Path) { // names are case-insensitive, as on Gitea
		case "/api/v1/orgs/acme/repos":
			_, _ = w.Write([]byte(`[
				{"name":"open-kit","full_name":"acme/open-kit","owner":{"login":"acme","visibility":"public"},"private":false,"default_branch":"main"},
				{"name":"secret-lab","full_name":"acme/secret-lab","owner":{"login":"acme","visibility":"public"},"private":true,"default_branch":"main"},
				{"name":"old-vault","full_name":"acme/old-vault","owner":{"login":"acme","visibility":"public"},"private":true,"archived":true},
				{"name":"inside","full_name":"acme/inside","owner":{"login":"acme","visibility":"public"},"internal":true}
			]`))
		case "/api/v1/orgs/limited-co/repos":
			_, _ = w.Write([]byte(`[{"name":"looks-public","full_name":"limited-co/looks-public","owner":{"login":"limited-co","visibility":"limited"},"private":false,"internal":false}]`))
		case "/api/v1/users/octo/repos":
			_, _ = w.Write([]byte(`[
				{"name":"tool","full_name":"octo/tool","owner":{"login":"octo","visibility":"public"}},
				{"name":"shared","full_name":"some-org/shared","owner":{"login":"some-org","visibility":"public"}}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := gitea.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGiteaDiscoverOrg(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	got, err := Gitea{Client: fakeGiteaOrg(t, 0), Cfg: config.GiteaScope{Type: "org", Name: "ACME"}}.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, s := range got {
		lines = append(lines, fmt.Sprintf("%s %s %s %v", s.Type, s.Slug, s.Visibility, s.Aliases))
	}
	want := "gitea ACME/open-kit public [acme/open-kit] | gitea ACME/secret-lab private [acme/secret-lab] | gitea ACME/inside internal [acme/inside]"
	if strings.Join(lines, " | ") != want {
		t.Errorf("sources:\n%s\nwant:\n%s", strings.Join(lines, " | "), want)
	}
	if strings.Contains(logs.String(), "old-vault") || !strings.Contains(logs.String(), "count=1") {
		t.Errorf("the archived private repo should be counted, not named:\n%s", logs.String())
	}
}

// A repo in a limited organisation is not public, though its own flags say
// neither private nor internal: the privacy guard must protect it.
func TestGiteaDiscoverLimitedOrg(t *testing.T) {
	got, err := Gitea{Client: fakeGiteaOrg(t, 0), Cfg: config.GiteaScope{Type: "org", Name: "limited-co"}}.Discover(context.Background())
	if err != nil || len(got) != 1 || got[0].Visibility != "internal" {
		t.Errorf("sources = %+v, %v; want looks-public as internal", got, err)
	}
}

// A user scope's repo owned by another login takes that login in its slug.
func TestGiteaDiscoverUserSlugs(t *testing.T) {
	got, err := Gitea{Client: fakeGiteaOrg(t, 0), Cfg: config.GiteaScope{Type: "user", Name: "Octo"}}.Discover(context.Background())
	if err != nil || len(got) != 2 || got[0].Slug != "Octo/tool" || got[1].Slug != "some-org/shared" {
		t.Errorf("sources = %+v, %v", got, err)
	}
}

// In exclude mode a skipped private repo is neither logged nor counted.
func TestGiteaDiscoverQuietPrivate(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	d := Gitea{Client: fakeGiteaOrg(t, 0), Cfg: config.GiteaScope{Type: "org", Name: "acme"}, Exclude: []string{"secret-*", "open-*"}, QuietPrivate: true}
	if _, err := d.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "(private)") || strings.Contains(logs.String(), "skipped archived") || !strings.Contains(logs.String(), "open-kit") {
		t.Errorf("want public skips only, no archived count:\n%s", logs.String())
	}
}

func TestGiteaDiscoverErrors(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		http.StatusNotFound:            func(err error) bool { var ce *config.ConfigError; return errors.As(err, &ce) },
		http.StatusUnauthorized:        func(err error) bool { var ae *config.AuthError; return errors.As(err, &ae) },
		http.StatusForbidden:           func(err error) bool { var ae *config.AuthError; return errors.As(err, &ae) },
		http.StatusTooManyRequests:     func(err error) bool { var re *config.RateLimitError; return errors.As(err, &re) },
		http.StatusInternalServerError: func(err error) bool { return strings.Contains(err.Error(), "HTTP 500") },
	} {
		_, err := Gitea{Client: fakeGiteaOrg(t, status), Cfg: config.GiteaScope{Type: "org", Name: "acme", TokenEnv: "GITEA_TOKEN"}}.Discover(context.Background())
		if err == nil || !check(err) || strings.Contains(err.Error(), "secret-lab") {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}
