package actions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
)

// glIssueFake answers project 5's issue and label endpoints as GitLab 19.4
// CE did, and records each request as "METHOD path?query body".
type glIssueFake struct {
	open, closed string // JSON listings
	labelStatus  int    // GET label answer
	labelCreate  int    // POST label answer, 201 when zero
	createLabels string // labels on a new issue, as GitLab returns them
	nextPage     bool   // every page of the open listing says there is another
	requests     []string
}

func (f *glIssueFake) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	line := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v4/projects/5")
	if r.URL.RawQuery != "" {
		line += "?" + r.URL.RawQuery
	}
	if len(body) > 0 {
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		keys := []string{}
		for _, k := range []string{"title", "labels", "state_event", "name", "color"} {
			if v, ok := m[k]; ok {
				keys = append(keys, k+"="+strings.Split(v.(string), "\n")[0])
			}
		}
		if d, ok := m["description"].(string); ok {
			keys = append(keys, "description="+strings.Split(d, "\n")[0])
		}
		line += " " + strings.Join(keys, " ")
	}
	f.requests = append(f.requests, line)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/5/issues":
		if r.URL.Query().Get("state") == "closed" {
			_, _ = w.Write([]byte(f.closed))
			return
		}
		if f.nextPage { // every page says there is another
			w.Header().Set("X-Next-Page", "2")
		}
		_, _ = w.Write([]byte(f.open))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/5/labels/baseliner":
		w.WriteHeader(f.labelStatus)
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/5/labels":
		if f.labelCreate == http.StatusConflict {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Label already exists"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/5/issues":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"iid":12,"title":"[baseliner] baseline compliance findings","labels":` + f.createLabels + `}`))
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v4/projects/5/issues/"):
		_, _ = w.Write([]byte(`{}`))
	default:
		http.NotFound(w, r)
	}
}

func runGitLab(t *testing.T, f *glIssueFake, status models.CheckStatus, username string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	c, err := gitlab.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	a := GitLabIssues{Client: c, Username: username, Now: func() time.Time { return time.Unix(0, 0) }, Sleep: func(time.Duration) {}}
	return a.Run(context.Background(), contractResult(status), 5)
}

// The GitLab adapter: the open issue is found by label and title, a new one
// is created with the label (created first when missing), a GitLab that
// drops the label gets the issue closed at once, updates and closes go by
// IID, a previous drop by the token's user blocks a new issue, and a listing
// cut short is an error, not "no issue".
func TestGitLabIssues(t *testing.T) {
	const title = `"title":"[baseliner] baseline compliance findings"`
	listed := `[{"iid":3,` + title + `,"description":"` + strings.ReplaceAll(BuildBody(contractResult(models.StatusFail), time.Unix(0, 0)), "\n", `\n`) + `","labels":["baseliner"]}]`
	for _, c := range []struct {
		name     string
		fake     glIssueFake
		status   models.CheckStatus
		username string
		want     []string
		errHas   string
	}{
		{"create, label missing", glIssueFake{open: `[]`, closed: `[]`, labelStatus: 404, createLabels: `["baseliner"]`}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /issues?author_username=bot&order_by=created_at&per_page=100&sort=desc&state=closed",
			"GET /labels/baseliner",
			"POST /labels name=baseliner color=#0075ca description=baseliner findings",
			"POST /issues title=[baseliner] baseline compliance findings labels=baseliner description=## baseliner findings",
		}, ""},
		{"label dropped on create", glIssueFake{open: `[]`, closed: `[]`, labelStatus: 200, createLabels: `[]`}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /issues?author_username=bot&order_by=created_at&per_page=100&sort=desc&state=closed",
			"GET /labels/baseliner",
			"POST /issues title=[baseliner] baseline compliance findings labels=baseliner description=## baseliner findings",
			`PUT /issues/12 state_event=close description=baseliner closed this issue: GitLab dropped the "baseliner" label, which it does when the token's user lacks Planner (or higher) access to the repo. Without the label, later runs cannot find it.`,
		}, "issue #12 was created without its label and closed"},
		{"label created meanwhile (409)", glIssueFake{open: `[]`, closed: `[]`, labelStatus: 404, labelCreate: http.StatusConflict, createLabels: `["baseliner"]`}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /issues?author_username=bot&order_by=created_at&per_page=100&sort=desc&state=closed",
			"GET /labels/baseliner",
			"POST /labels name=baseliner color=#0075ca description=baseliner findings",
			"POST /issues title=[baseliner] baseline compliance findings labels=baseliner description=## baseliner findings",
		}, ""},
		{"closed before, not for a dropped label", glIssueFake{open: `[]`, closed: `[{"iid":4,` + title + `,"description":"✅ All baseline checks pass"}]`, labelStatus: 200, createLabels: `["baseliner"]`}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /issues?author_username=bot&order_by=created_at&per_page=100&sort=desc&state=closed",
			"GET /labels/baseliner",
			"POST /issues title=[baseliner] baseline compliance findings labels=baseliner description=## baseliner findings",
		}, ""},
		{"dropped before", glIssueFake{open: `[]`, closed: `[{"iid":4,` + title + `,"description":"baseliner closed this issue: x"}]`}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /issues?author_username=bot&order_by=created_at&per_page=100&sort=desc&state=closed",
		}, `GitLab dropped the "baseliner" label from #4`},
		{"another case is another label", glIssueFake{open: `[]`, closed: `[]`, labelStatus: 200, createLabels: `["Baseliner"]`}, models.StatusFail, "bot", nil,
			"issue #12 was created without its label and closed"},
		{"no username: no dropped search", glIssueFake{open: `[]`, labelStatus: 200, createLabels: `["baseliner"]`}, models.StatusFail, "", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"GET /labels/baseliner",
			"POST /issues title=[baseliner] baseline compliance findings labels=baseliner description=## baseliner findings",
		}, ""},
		{"update", glIssueFake{open: listed}, models.StatusFail, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"PUT /issues/3 description=## baseliner findings",
		}, ""},
		{"close", glIssueFake{open: listed}, models.StatusPass, "bot", []string{
			"GET /issues?labels=baseliner&per_page=100&state=opened",
			"PUT /issues/3 state_event=close description=## baseliner findings",
		}, ""},
		{"listing cut short", glIssueFake{open: `[{"iid":1,"title":"other"}]`, nextPage: true}, models.StatusPass, "bot", nil, "more than 10 pages"},
	} {
		f := c.fake
		err := runGitLab(t, &f, c.status, c.username)
		switch {
		case c.errHas == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.errHas != "" && (err == nil || !strings.Contains(err.Error(), c.errHas)):
			t.Errorf("%s: error %v, want %q", c.name, err, c.errHas)
		}
		if c.want != nil && strings.Join(f.requests, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s: requests\n  %s\nwant\n  %s", c.name, strings.Join(f.requests, "\n  "), strings.Join(c.want, "\n  "))
		}
	}
}
