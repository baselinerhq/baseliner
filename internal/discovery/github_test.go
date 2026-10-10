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

func TestIncludeExcludeGlobs(t *testing.T) {
	d := GitHub{Include: []string{"svc-*", "lib-*"}, Exclude: []string{"*-old", "archived-*"}}

	cases := []struct {
		name             string
		excluded, includ bool
	}{
		{"svc-api", false, true},
		{"lib-core", false, true},
		{"docs", false, false},      // not in include
		{"svc-api-old", true, true}, // excluded wins at the call site
		{"archived-thing", true, false},
	}
	for _, c := range cases {
		if got := d.isExcluded(c.name); got != c.excluded {
			t.Errorf("isExcluded(%q) = %v, want %v", c.name, got, c.excluded)
		}
		if got := d.isIncluded(c.name); got != c.includ {
			t.Errorf("isIncluded(%q) = %v, want %v", c.name, got, c.includ)
		}
	}
}

func TestEmptyIncludeMatchesAll(t *testing.T) {
	d := GitHub{} // no include/exclude
	if !d.isIncluded("anything") {
		t.Error("empty include should match all")
	}
	if d.isExcluded("anything") {
		t.Error("empty exclude should match none")
	}
}

// globMatch must follow fnmatch semantics, not Go's path.Match. These cases are
// exactly where the two engines disagree (verified against Python's fnmatch).
func TestGlobMatchFnmatchSemantics(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		// Negated character class uses `!`, not `^`.
		{"[!abc]", "a", false},
		{"[!abc]", "x", true},
		{"svc-[!0-9]", "svc-x", true},
		{"svc-[!0-9]", "svc-5", false},
		// A bare `^` inside a class is literal in fnmatch (not negation).
		{"[^abc]", "^", true},
		{"[^abc]", "x", false},
		// Malformed (unterminated) class is treated literally and can still match.
		{"svc-[abc", "svc-[abc", true},
		{"svc-[abc", "svc-a", false},
		// Ordinary wildcards behave as expected.
		{"svc-*", "svc-api", true},
		{"*-old", "thing-old", true},
		{"lib-?", "lib-x", true},
		{"lib-?", "lib-xy", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// Debug lines name a repo only when it is known to be public, the same rule
// the privacy guard applies: an unrecognised visibility is not public.
func TestLogNameHidesAllButPublic(t *testing.T) {
	for _, c := range []struct {
		repo *github.Repository
		want string
	}{
		{&github.Repository{Name: github.Ptr("a"), Visibility: github.Ptr("public")}, "a"},
		{&github.Repository{Name: github.Ptr("a")}, "a"}, // listing omitted visibility, not private
		{&github.Repository{Name: github.Ptr("a"), Visibility: github.Ptr("private")}, "(private)"},
		{&github.Repository{Name: github.Ptr("a"), Visibility: github.Ptr("internal")}, "(private)"},
		{&github.Repository{Name: github.Ptr("a"), Visibility: github.Ptr("limited")}, "(private)"},
		{&github.Repository{Name: github.Ptr("a"), Private: github.Ptr(true)}, "(private)"},
	} {
		if got := logName(c.repo); got != c.want {
			t.Errorf("logName(visibility=%q private=%v) = %q, want %q",
				c.repo.GetVisibility(), c.repo.GetPrivate(), got, c.want)
		}
	}
}

// On an instance that hides public repos from anonymous visitors (GitHub
// Enterprise Server in private mode), the API still calls them public. One
// anonymous read of a public repo (never a private one, which no visitor can
// read) decides: if it fails, every public repo is internal, so no
// log line or source names it. It runs before filtering, so a filtered-out
// public repo is not named either.
func TestGitHubPublicHiddenInstance(t *testing.T) {
	for _, anonStatus := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusFound} {
		var anonReads int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v3/orgs/acme/repos":
				_, _ = w.Write([]byte(`[
					{"name":"priv","owner":{"login":"acme"},"private":true,"visibility":"private"},
					{"name":"pub-skip","owner":{"login":"acme"},"visibility":"public"},
					{"name":"pub","owner":{"login":"acme"},"visibility":"public"}]`))
			case r.URL.Path == "/api/v3/elsewhere":
				_, _ = w.Write([]byte(`{"name":"pub-skip"}`))
			case r.URL.Path == "/api/v3/repos/acme/pub-skip" && r.Header.Get("Authorization") == "":
				anonReads++
				if anonStatus == http.StatusFound {
					// To a page that answers 200 with the repo itself.
					w.Header().Set("Location", "/api/v3/elsewhere")
				}
				w.WriteHeader(anonStatus)
				_, _ = w.Write([]byte(`{"name":"pub-skip"}`))
			default:
				http.NotFound(w, r)
			}
		}))
		c := github.NewClient(nil).WithAuthToken("tok")
		c.BaseURL, _ = url.Parse(srv.URL + "/api/v3/")
		var logs bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
		d := GitHub{Client: c, Cfg: config.GitHubScope{Type: "org", Name: "acme"}, Exclude: []string{"pub-skip"}}
		sources, err := d.Discover(context.Background())
		slog.SetDefault(prev)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		hidden := anonStatus != http.StatusOK
		want := "public"
		if hidden {
			want = "internal"
		}
		for _, s := range sources {
			if r := s.GitHubRepo.(*github.Repository); r.GetName() == "pub" && r.GetVisibility() != want {
				t.Errorf("anonymous %d: pub visibility %q, want %q", anonStatus, r.GetVisibility(), want)
			}
		}
		if anonReads != 1 {
			t.Errorf("anonymous %d: %d anonymous reads, want 1", anonStatus, anonReads)
		}
		if strings.Contains(logs.String(), "pub-skip") == hidden {
			t.Errorf("anonymous %d: pub-skip named = %v\n%s", anonStatus, !hidden, logs.String())
		}
	}
}

// github.com has no private mode, so it is not asked.
func TestGitHubPublicHiddenSkipsDotCom(t *testing.T) {
	d := GitHub{Client: github.NewClient(nil)}
	repos := []*github.Repository{{Name: github.Ptr("pub"), Owner: &github.User{Login: github.Ptr("acme")}, Visibility: github.Ptr("public")}}
	if hidden, err := d.publicHidden(context.Background(), repos); hidden || err != nil {
		t.Errorf("github.com: hidden %v, err %v", hidden, err)
	}
	for _, raw := range []string{"https://api.github.com/", "https://API.GitHub.com/", "https://api.github.com:443/", "https://api.github.com./"} {
		u, _ := url.Parse(raw)
		if !isDotCom(u) {
			t.Errorf("isDotCom(%s) = false", raw)
		}
	}
	for _, raw := range []string{"https://ghe.example.com/api/v3/", "https://api.github.com.evil.example/"} {
		u, _ := url.Parse(raw)
		if isDotCom(u) {
			t.Errorf("isDotCom(%s) = true", raw)
		}
	}
}

// The anonymous read sends no credential, not even one in the API URL; a 200
// counts only when its body is the repo; a refusal is hidden; anything else
// is retried once and then an error, so a blip never changes a scan.
func TestGitHubAnonymousProbeAnswers(t *testing.T) {
	repos := []*github.Repository{{Name: github.Ptr("pub"), Owner: &github.User{Login: github.Ptr("acme")}, Visibility: github.Ptr("public")}}
	for _, c := range []struct {
		name    string
		answers []func(w http.ResponseWriter)
		hidden  bool
		err     bool
	}{
		{"repo", []func(http.ResponseWriter){jsonBody(`{"name":"pub"}`)}, false, false},
		{"sign-in page", []func(http.ResponseWriter){func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>Sign in</html>")) }}, true, false},
		{"another repo", []func(http.ResponseWriter){jsonBody(`{"name":"other"}`)}, true, false},
		{"refused", []func(http.ResponseWriter){status(http.StatusForbidden)}, true, false},
		{"blip then repo", []func(http.ResponseWriter){status(http.StatusServiceUnavailable), jsonBody(`{"name":"pub"}`)}, false, false},
		{"two blips", []func(http.ResponseWriter){status(http.StatusServiceUnavailable), status(http.StatusBadGateway)}, false, true},
		{"rate limited", []func(http.ResponseWriter){func(w http.ResponseWriter) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		}}, false, true},
	} {
		n := 0
		var auth []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = append(auth, r.Header.Get("Authorization"))
			c.answers[min(n, len(c.answers)-1)](w)
			n++
		}))
		u, _ := url.Parse(srv.URL + "/api/v3/")
		u.User = url.UserPassword("svc", "pw")
		cl := github.NewClient(nil).WithAuthToken("tok")
		cl.BaseURL = u
		hidden, err := GitHub{Client: cl}.publicHidden(context.Background(), repos)
		srv.Close()
		if hidden != c.hidden || (err != nil) != c.err {
			t.Errorf("%s: hidden %v, err %v", c.name, hidden, err)
		}
		for _, a := range auth {
			if a != "" {
				t.Errorf("%s: probe sent Authorization %q", c.name, a)
			}
		}
	}
}

func jsonBody(b string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write([]byte(b)) }
}

func status(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

// A probe that cannot reach the instance says why without naming the repo.
func TestGitHubProbeErrorNamesNoRepo(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(srv.URL + "/api/v3/")
	srv.Close() // nothing listens there now
	cl := github.NewClient(nil)
	cl.BaseURL = u
	repos := []*github.Repository{{Name: github.Ptr("hidden-name"), Owner: &github.User{Login: github.Ptr("acme")}, Visibility: github.Ptr("public")}}
	_, err := GitHub{Client: cl}.publicHidden(context.Background(), repos)
	var ce *config.ConfigError
	if !errors.As(err, &ce) || strings.Contains(err.Error(), "hidden-name") || !strings.Contains(err.Error(), "unauthenticated API requests") {
		t.Errorf("err = %v; want a config error that says what to check and does not name the repo", err)
	}
}
