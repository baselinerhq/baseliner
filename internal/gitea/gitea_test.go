package gitea

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A repo is only as visible as its owner: Forgejo reports private=false and
// internal=false for a repo in a limited organisation, which anonymous users
// still cannot see (seen on Forgejo 16).
func TestVisibility(t *testing.T) {
	for _, c := range []struct {
		repo Repo
		want string
	}{
		{Repo{Owner: Owner{Visibility: "public"}}, "public"},
		{Repo{}, "public"}, // an owner without a visibility field
		{Repo{Private: true, Owner: Owner{Visibility: "public"}}, "private"},
		{Repo{Internal: true, Owner: Owner{Visibility: "public"}}, "internal"},
		{Repo{Owner: Owner{Visibility: "limited"}}, "internal"},
		{Repo{Owner: Owner{Visibility: "Limited"}}, "internal"},
		{Repo{Owner: Owner{Visibility: "private"}}, "private"},
		{Repo{Internal: true, Owner: Owner{Visibility: "private"}}, "private"},
		{Repo{Owner: Owner{Visibility: "secret"}}, "private"},
	} {
		if got := Visibility(c.repo); got != c.want {
			t.Errorf("Visibility(%+v) = %q, want %q", c.repo, got, c.want)
		}
	}
}

// Listings are read by page number until a short page; the Link header,
// which names the instance's own URL, is not followed.
func TestReposPaginate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/acme/repos" || r.Header.Get("Authorization") != "token tok" {
			http.Error(w, `{"message":"no"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Link", `<https://elsewhere.example/api/v1/orgs/acme/repos?page=2>; rel="next"`)
		n := pageSize
		if r.URL.Query().Get("page") == "2" {
			n = 3
		}
		var items []string
		for i := range n {
			items = append(items, fmt.Sprintf(`{"name":"r%s-%d"}`, r.URL.Query().Get("page"), i))
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	})
	repos, complete, err := c.Repos(context.Background(), "org", "acme", 5)
	if err != nil || !complete || len(repos) != pageSize+3 {
		t.Errorf("Repos = %d, %v, %v", len(repos), complete, err)
	}
	if _, complete, _ := c.Repos(context.Background(), "org", "acme", 1); complete {
		t.Error("a capped listing must not be complete")
	}
}

// A directory lists as an array; a file at that path comes back as an object,
// which means no directory there.
func TestContents(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v1/repos/o/r/contents/docs":
			_, _ = w.Write([]byte(`[{"path":"docs/a.md","type":"file"},{"path":"docs/img","type":"dir"}]`))
		case "/api/v1/repos/o/r/contents/LICENSE":
			_, _ = w.Write([]byte(`{"name":"LICENSE","path":"LICENSE","type":"file"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"GetContentsOrList","errors":["object does not exist [id: , rel_path: secret/path]"]}`))
		}
	})
	es, isDir, err := c.Contents(context.Background(), "o", "r", "main", "docs")
	if err != nil || !isDir || len(es) != 2 {
		t.Errorf("dir: %v %v %v", es, isDir, err)
	}
	if es, isDir, err := c.Contents(context.Background(), "o", "r", "main", "LICENSE"); err != nil || isDir || es != nil {
		t.Errorf("file: %v %v %v", es, isDir, err)
	}
	_, _, err = c.Contents(context.Background(), "o", "r", "main", ".circleci")
	if Status(err) != http.StatusNotFound || strings.Contains(err.Error(), "secret/path") || strings.Contains(err.Error(), "o/r") {
		t.Errorf("missing: %v; want a 404 naming neither the repo nor the server's error list", err)
	}
}

// Errors name the endpoint without the owner or repo, on one bounded line.
func TestErrorText(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"message":"forbidden\nfor %s"}`, strings.Repeat("x", 500))
	})
	_, err := c.RawFile(context.Background(), "acme", "secret-lab", "main", "team/secret.yml", 10)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "acme") ||
		strings.Contains(err.Error(), "\n") || len(err.Error()) > 300 || !strings.Contains(err.Error(), "/repos/(repo)/raw/(path)") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := c.Repos(context.Background(), "org", "acme", 1); err == nil || strings.Contains(err.Error(), "acme") {
		t.Errorf("listing err = %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Branches(context.Background(), "o", "r", 10)
	var rl interface{ RateLimit() (string, time.Time) }
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v is not a rate-limit refusal", err)
	}
	if forge, reset := rl.RateLimit(); forge != "Gitea" || time.Until(reset) < 50*time.Second {
		t.Errorf("RateLimit = %s %v", forge, reset)
	}
}

// A redirect away from the API is not followed, so the token stays there.
func TestRedirectsStayInAPI(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer other.Close()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	})
	if _, err := c.Branches(context.Background(), "o", "r", 10); err == nil || leaked.Load() != 0 {
		t.Errorf("err %v, token sent %d time(s)", err, leaked.Load())
	}
}

func TestRawFileLimit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/repos/o/r/raw/docs/README.md" || r.URL.Query().Get("ref") != "main" {
			http.Error(w, `{"message":"The target couldn't be found."}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	})
	b, err := c.RawFile(context.Background(), "o", "r", "main", "docs/README.md", 10)
	if err != nil || len(b) != 10 {
		t.Errorf("RawFile = %d bytes, %v", len(b), err)
	}
}
