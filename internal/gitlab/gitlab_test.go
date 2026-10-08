package gitlab

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

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/", "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

// The group is looked up by its full path, sent as one escaped segment, with
// the token as a bearer credential.
func TestGroupLookup(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/groups/acme%2Fplatform" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"message":"404 Group Not Found"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":7,"full_path":"acme/platform"}`))
	})
	g, err := c.Group(context.Background(), "acme/platform")
	if err != nil || g.ID != 7 || g.FullPath != "acme/platform" {
		t.Fatalf("Group = %+v, %v", g, err)
	}
}

// Listings follow GitLab's Link header, or X-Next-Page when there is none.
func TestPagination(t *testing.T) {
	for _, style := range []string{"link", "x-next-page"} {
		t.Run(style, func(t *testing.T) {
			var srvURL string
			c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("include_subgroups") != "true" || q.Get("with_shared") != "false" {
					http.Error(w, "bad query", http.StatusBadRequest)
					return
				}
				switch q.Get("page") {
				case "":
					if style == "link" {
						w.Header().Set("Link", fmt.Sprintf(`<%s/api/v4/groups/7/projects?page=2&per_page=100&include_subgroups=true&with_shared=false>; rel="next", <%s/x>; rel="last"`, srvURL, srvURL))
					} else {
						w.Header().Set("X-Next-Page", "2")
					}
					_, _ = w.Write([]byte(`[{"id":1,"path_with_namespace":"acme/a","visibility":"public"}]`))
				case "2":
					_, _ = w.Write([]byte(`[{"id":2,"path_with_namespace":"acme/sub/b","visibility":"private"}]`))
				}
			})
			srvURL = srv.URL
			ps, complete, err := c.GroupProjects(context.Background(), 7, 10)
			if err != nil || !complete || len(ps) != 2 || ps[1].PathWithNamespace != "acme/sub/b" {
				t.Fatalf("GroupProjects = %+v, %v, %v", ps, complete, err)
			}
		})
	}
}

// A next-page link to another host is not followed: the token would go with
// it.
func TestPaginationStaysOnHost(t *testing.T) {
	var foreign atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		foreign.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer other.Close()
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/api/v4/groups/7/projects?page=2>; rel="next"`, other.URL))
		_, _ = w.Write([]byte(`[{"id":1}]`))
	})
	_, complete, err := c.GroupProjects(context.Background(), 7, 10)
	if err == nil || complete || foreign.Load() != 0 {
		t.Errorf("err = %v, complete = %v, foreign requests = %d", err, complete, foreign.Load())
	}
}

// Behind a proxy or an internal hostname, GitLab's Link header names the
// instance's external URL. The page number in X-Next-Page is followed on this
// API instead, and the other host is never contacted.
func TestPaginationPrefersPageNumber(t *testing.T) {
	var foreign atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		foreign.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer other.Close()
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v4/projects/1/repository/branches?page=2>; rel="next"`, other.URL))
			_, _ = w.Write([]byte(`[{"name":"a"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"name":"b"}]`))
	})
	bs, err := c.Branches(context.Background(), 1, 200)
	if err != nil || fmt.Sprint(bs) != "[a b]" || foreign.Load() != 0 {
		t.Errorf("Branches = %v, %v; foreign requests %d", bs, err, foreign.Load())
	}
}

// More pages than allowed is reported as incomplete, not as the whole list.
func TestTreePageCap(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Next-Page", "2")
		_, _ = w.Write([]byte(`[{"path":"a","type":"blob","mode":"100644"}]`))
	})
	es, complete, err := c.Tree(context.Background(), 1, "main", "", 3)
	if err != nil || complete || len(es) != 3 {
		t.Errorf("Tree = %d entries, complete %v, %v", len(es), complete, err)
	}
}

// Errors carry the numeric request path and a bounded one-line message,
// never a project path; a file's path is left out of its error too.
func TestErrorText(t *testing.T) {
	long := strings.Repeat("x", 500)
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprintf(w, `{"message":"403 Forbidden\nfor %s"}`, long)
	})
	_, _, err := c.Tree(context.Background(), 42, "main", "secret-dir", 1)
	if err == nil || !strings.Contains(err.Error(), "GitLab GET /projects/42/repository/tree: 403 403 Forbidden for x") ||
		strings.Contains(err.Error(), "\n") || len(err.Error()) > 300 || strings.Contains(err.Error(), "secret-dir") {
		t.Errorf("err = %v", err)
	}
	_, err = c.RawFile(context.Background(), 42, "main", "team/secret.yml", 10)
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "/projects/42/repository/files/(path)/raw") {
		t.Errorf("RawFile err = %v", err)
	}
	if Status(err) != http.StatusForbidden {
		t.Errorf("Status = %d", Status(err))
	}
}

// A 429 is a rate-limit refusal with the reset GitLab gave.
func TestRateLimit(t *testing.T) {
	at := time.Now().Add(time.Hour).Truncate(time.Second)
	for name, h := range map[string]map[string]string{
		"RateLimit-Reset": {"RateLimit-Reset": fmt.Sprint(at.Unix())},
		"Retry-After":     {"Retry-After": "3600"},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range h {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			})
			_, err := c.Branches(context.Background(), 1, 100)
			var rl interface{ RateLimit() (string, time.Time) }
			if !errors.As(err, &rl) {
				t.Fatalf("err = %v is not a rate-limit refusal", err)
			}
			forge, reset := rl.RateLimit()
			if forge != "GitLab" || reset.Sub(at).Abs() > 5*time.Second || Status(err) != http.StatusTooManyRequests {
				t.Errorf("RateLimit = %s %v, want GitLab %v", forge, reset, at)
			}
		})
	}
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	_, err := c.Branches(context.Background(), 1, 100)
	var rl interface{ RateLimit() (string, time.Time) }
	if errors.As(err, &rl) {
		t.Errorf("a 403 is not a rate-limit refusal: %v", err)
	}
}

// A missing file or directory is absent; a project that cannot be read is
// not.
func TestIsAbsent(t *testing.T) {
	for msg, want := range map[string]bool{"404 Tree Not Found": true, "404 File Not Found": true, "404 Project Not Found": false} {
		c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"message":%q}`, msg)
		})
		_, _, err := c.Tree(context.Background(), 1, "main", "docs", 1)
		if IsAbsent(err) != want {
			t.Errorf("%s: IsAbsent = %v", msg, !want)
		}
	}
	if IsAbsent(&Error{Status: http.StatusInternalServerError}) || IsAbsent(errors.New("x")) {
		t.Error("only a 404 is absent")
	}
}

func TestRawFileLimitAndPath(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/projects/5/repository/files/docs%2FREADME.md/raw" || r.URL.Query().Get("ref") != "main" {
			http.Error(w, `{"message":"404 File Not Found"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	})
	b, err := c.RawFile(context.Background(), 5, "main", "docs/README.md", 10)
	if err != nil || len(b) != 10 {
		t.Errorf("RawFile = %d bytes, %v", len(b), err)
	}
}

func TestBranchesCap(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Next-Page", "2") // always more
		var names []string
		for i := range 100 {
			names = append(names, fmt.Sprintf(`{"name":"b%d"}`, i))
		}
		_, _ = w.Write([]byte("[" + strings.Join(names, ",") + "]"))
	})
	bs, err := c.Branches(context.Background(), 1, 30)
	if err != nil || len(bs) != 30 {
		t.Errorf("Branches = %d, %v", len(bs), err)
	}
}

func TestVisibility(t *testing.T) {
	for in, want := range map[string]string{
		"public": "public", "Internal": "internal", " PRIVATE ": "private", "": "private", "secret": "private",
	} {
		if got := Visibility(Project{Visibility: in}); got != want {
			t.Errorf("Visibility(%q) = %q, want %q", in, got, want)
		}
	}
}
