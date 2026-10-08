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
	"github.com/baselinerhq/baseliner/internal/gitlab"
)

// fakeGitLabGroup serves group "acme" (id 1) with projects at several depths,
// one archived, one without a visibility, and one outside the group.
func fakeGitLabGroup(t *testing.T, groupStatus int) *gitlab.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/groups/acme", "/api/v4/groups/ACME":
			if groupStatus != 0 {
				w.WriteHeader(groupStatus)
				_, _ = w.Write([]byte(`{"message":"detail naming acme/team/secret-lab"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":1,"full_path":"acme"}`))
		case "/api/v4/groups/1/projects":
			_, _ = w.Write([]byte(`[
				{"id":10,"path":"open-kit","path_with_namespace":"acme/open-kit","visibility":"public"},
				{"id":11,"path":"secret-lab","path_with_namespace":"acme/team/secret-lab","visibility":"private"},
				{"id":12,"path":"inside","path_with_namespace":"acme/inside","visibility":"internal"},
				{"id":13,"path":"old-vault","path_with_namespace":"acme/old-vault","visibility":"private","archived":true},
				{"id":14,"path":"hidden-x","path_with_namespace":"acme/team/hidden-x","visibility":"private"},
				{"id":15,"path":"novis","path_with_namespace":"acme/sub/deep/novis"},
				{"id":16,"path":"elsewhere","path_with_namespace":"other/elsewhere","visibility":"private"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := gitlab.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGitLabDiscover(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	d := GitLab{Client: fakeGitLabGroup(t, 0), Cfg: config.GitLabScope{Group: "ACME"}, Exclude: []string{"team/hidden-*"}}
	got, err := d.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, s := range got {
		lines = append(lines, fmt.Sprintf("%s %s %s %v", s.Type, s.Slug, s.Visibility, s.Aliases))
	}
	want := []string{
		"gitlab acme/open-kit public [acme%2Fopen-kit ACME/open-kit ACME%2Fopen-kit]",
		"gitlab acme/team/secret-lab private [acme%2Fteam%2Fsecret-lab ACME/team/secret-lab ACME%2Fteam%2Fsecret-lab]",
		"gitlab acme/inside internal [acme%2Finside ACME/inside ACME%2Finside]",
		"gitlab acme/sub/deep/novis private [acme%2Fsub%2Fdeep%2Fnovis ACME/sub/deep/novis ACME%2Fsub%2Fdeep%2Fnovis]",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("sources:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// Projects not scanned are never named unless public.
	for _, name := range []string{"old-vault", "hidden-x", "elsewhere"} {
		if strings.Contains(logs.String(), name) {
			t.Errorf("the log names %s:\n%s", name, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "skipped archived projects") || !strings.Contains(logs.String(), "count=1") {
		t.Errorf("no archived count:\n%s", logs.String())
	}
}

func TestGitLabDiscoverIncludeAndArchived(t *testing.T) {
	d := GitLab{Client: fakeGitLabGroup(t, 0), Cfg: config.GitLabScope{Group: "acme", IncludeArchived: true}, Include: []string{"old-*", "team/*"}}
	got, err := d.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, s := range got {
		slugs = append(slugs, s.Slug)
		if len(s.Aliases) != 1 {
			t.Errorf("%s: the config spells the group as GitLab does, so only the encoded alias: %v", s.Slug, s.Aliases)
		}
	}
	if fmt.Sprint(slugs) != "[acme/team/secret-lab acme/old-vault acme/team/hidden-x]" {
		t.Errorf("slugs = %v", slugs)
	}
}

// A missing group is a config error, a rejected token an auth error, and any
// other failure is reported by status alone: discovery output is not
// redacted, and the server's message could name a project.
func TestGitLabDiscoverErrors(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		http.StatusNotFound:            func(err error) bool { var ce *config.ConfigError; return errors.As(err, &ce) },
		http.StatusUnauthorized:        func(err error) bool { var ae *config.AuthError; return errors.As(err, &ae) },
		http.StatusForbidden:           func(err error) bool { var ae *config.AuthError; return errors.As(err, &ae) },
		http.StatusTooManyRequests:     func(err error) bool { var re *config.RateLimitError; return errors.As(err, &re) },
		http.StatusInternalServerError: func(err error) bool { return strings.Contains(err.Error(), "HTTP 500") },
	} {
		_, err := GitLab{Client: fakeGitLabGroup(t, status), Cfg: config.GitLabScope{Group: "acme", TokenEnv: "GITLAB_TOKEN"}}.Discover(context.Background())
		if err == nil || !check(err) || strings.Contains(err.Error(), "secret-lab") {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}

// QuietPrivate leaves skipped non-public projects out of the log; public ones
// are still logged.
func TestGitLabDiscoverQuietPrivate(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	d := GitLab{Client: fakeGitLabGroup(t, 0), Cfg: config.GitLabScope{Group: "acme"}, Exclude: []string{"team/*", "open-*"}, QuietPrivate: true}
	if _, err := d.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "(private)") || !strings.Contains(logs.String(), "acme/open-kit") {
		t.Errorf("want public skips only:\n%s", logs.String())
	}
}
