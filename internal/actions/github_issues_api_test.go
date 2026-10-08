package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
)

// fakeGitHub wires a go-github client to an httptest server.
func fakeGitHub(t *testing.T, h http.Handler) *github.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := github.NewClient(nil)
	u, _ := url.Parse(srv.URL + "/")
	c.BaseURL = u
	return c
}

func decodeBody(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

// findingResult has a failing check (so the issue should be opened/updated).
func findingResult() models.RepoResult {
	return models.RepoResult{
		Slug:  "o/r",
		Score: models.ScorePtr(0.6),
		Results: []models.CheckResult{
			{CheckID: "license_exists", Status: models.StatusFail, Severity: models.SeverityHigh, Message: sp("No LICENSE")},
		},
	}
}

// compliantResult passes every check (so any open issue should be closed).
func compliantResult() models.RepoResult {
	return models.RepoResult{
		Slug:  "o/r",
		Score: models.ScorePtr(1.0),
		Results: []models.CheckResult{
			{CheckID: "readme_exists", Status: models.StatusPass, Severity: models.SeverityCritical},
		},
	}
}

// noWait builds a GitHubIssues with deterministic now and a no-op sleep so the
// 1.1s mutation spacing doesn't slow tests.
func noWait(c *github.Client, dryRun bool) GitHubIssues {
	return GitHubIssues{
		Client: c,
		DryRun: dryRun,
		Now:    func() time.Time { return time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC) },
		Sleep:  func(time.Duration) {},
	}
}

func TestRunCreatesIssueWhenFindings(t *testing.T) {
	var created, edited bool
	var labelsApplied []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`)) // no existing issue
	})
	mux.HandleFunc("POST /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		created = true
		var req github.IssueRequest
		decodeBody(t, r, &req)
		if req.Labels != nil {
			labelsApplied = *req.Labels
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7}`))
	})
	mux.HandleFunc("PATCH /repos/o/r/issues/{n}", func(w http.ResponseWriter, _ *http.Request) {
		edited = true
		_, _ = w.Write([]byte(`{}`))
	})

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), findingResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !created || edited {
		t.Errorf("expected create only, got created=%v edited=%v", created, edited)
	}
	if len(labelsApplied) != 1 || labelsApplied[0] != "baseliner" {
		t.Errorf("expected baseliner label applied, got %v", labelsApplied)
	}
}

func TestRunUpdatesIssueWhenFindings(t *testing.T) {
	var created bool
	var patchState *string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":42,"title":"[baseliner] baseline compliance findings"}]`))
	})
	mux.HandleFunc("POST /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":99}`))
	})
	mux.HandleFunc("PATCH /repos/o/r/issues/42", func(w http.ResponseWriter, r *http.Request) {
		var req github.IssueRequest
		decodeBody(t, r, &req)
		patchState = req.State
		_, _ = w.Write([]byte(`{"number":42}`))
	})

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), findingResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if created {
		t.Error("expected update, not create")
	}
	if patchState != nil && *patchState == "closed" {
		t.Error("updating a repo with findings must not close its issue")
	}
}

func TestRunClosesIssueWhenCompliant(t *testing.T) {
	var closed bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"number":42,"title":"[baseliner] baseline compliance findings"}]`))
	})
	mux.HandleFunc("PATCH /repos/o/r/issues/42", func(w http.ResponseWriter, r *http.Request) {
		var req github.IssueRequest
		decodeBody(t, r, &req)
		if req.State != nil && *req.State == "closed" {
			closed = true
		}
		_, _ = w.Write([]byte(`{"number":42,"state":"closed"}`))
	})
	// No label/create handlers: a compliant repo must not touch them.

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), compliantResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !closed {
		t.Error("expected the existing issue to be closed when the repo is compliant")
	}
}

func TestRunNoopWhenCompliantAndNoIssue(t *testing.T) {
	var wrote bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	write := func(w http.ResponseWriter, _ *http.Request) { wrote = true; w.WriteHeader(http.StatusOK) }
	mux.HandleFunc("POST /repos/o/r/issues", write)
	mux.HandleFunc("PATCH /repos/o/r/issues/{n}", write)
	mux.HandleFunc("POST /repos/o/r/labels", write)

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), compliantResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if wrote {
		t.Error("a compliant repo with no existing issue must perform no writes")
	}
}

func TestRunDryRunSkipsCreate(t *testing.T) {
	var wrote bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	write := func(w http.ResponseWriter, _ *http.Request) { wrote = true; w.WriteHeader(http.StatusCreated) }
	mux.HandleFunc("POST /repos/o/r/labels", write)
	mux.HandleFunc("POST /repos/o/r/issues", write)
	mux.HandleFunc("PATCH /repos/o/r/issues/{n}", write)

	if err := noWait(fakeGitHub(t, mux), true).Run(context.Background(), findingResult(), "o", "r"); err != nil {
		t.Fatalf("Run (dry-run): %v", err)
	}
	if wrote {
		t.Error("dry-run must not perform any write calls")
	}
}

func TestEnsureLabelCreatesWhenMissing(t *testing.T) {
	var createdLabel bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("POST /repos/o/r/labels", func(w http.ResponseWriter, r *http.Request) {
		createdLabel = true
		var lbl github.Label
		decodeBody(t, r, &lbl)
		if lbl.GetName() != "baseliner" || lbl.GetColor() != "0075ca" {
			t.Errorf("unexpected label payload: name=%q color=%q", lbl.GetName(), lbl.GetColor())
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	})

	if err := noWait(fakeGitHub(t, mux), false).ensureLabel(context.Background(), "o", "r"); err != nil {
		t.Errorf("ensureLabel: %v", err)
	}
	if !createdLabel {
		t.Error("expected label to be created when missing")
	}
}

// findExisting searches by label, so an issue opened without it is never found
// again and every later run opens another (#112). A label that cannot be
// ensured is a delivery failure, not a reason to create the issue unlabelled.
func TestRunDoesNotCreateUnlabelledIssueWhenLabelFails(t *testing.T) {
	var opened []github.IssueRequest
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("POST /repos/o/r/labels", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != "baseliner" {
			t.Errorf("issue search not scoped by label: %q", r.URL.RawQuery)
		}
		var labelled []map[string]any
		for i, req := range opened {
			if req.Labels != nil && len(*req.Labels) > 0 {
				labelled = append(labelled, map[string]any{"number": i + 1, "title": req.GetTitle()})
			}
		}
		_ = json.NewEncoder(w).Encode(labelled)
	})
	mux.HandleFunc("POST /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		var req github.IssueRequest
		decodeBody(t, r, &req)
		opened = append(opened, req)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": len(opened)})
	})

	action := noWait(fakeGitHub(t, mux), false)
	for run := 1; run <= 2; run++ {
		if err := action.Run(context.Background(), findingResult(), "o", "r"); err == nil {
			t.Errorf("run %d: Run returned nil although the label could not be created", run)
		}
	}
	if len(opened) != 0 {
		t.Errorf("opened %d issue(s) without the label; later runs cannot find them", len(opened))
	}
}

func TestFindExistingMatchesByTitleAcrossPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"number":5,"title":"[baseliner] baseline compliance findings"}]`))
			return
		}
		w.Header().Set("Link", `<`+r.URL.Path+`?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"number":1,"title":"something else"}]`))
	})

	got, err := noWait(fakeGitHub(t, mux), false).findExisting(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("findExisting: %v", err)
	}
	if got == nil || got.GetNumber() != 5 {
		t.Fatalf("expected to find issue #5 on page 2, got %v", got)
	}
}

// A failed issue search is a delivery failure, not "no issue": treating it as
// "no issue" skipped the close for a now-compliant repo, and for a repo with
// findings it would create a duplicate.
func TestRunReturnsErrorWhenSearchFails(t *testing.T) {
	for name, result := range map[string]models.RepoResult{"compliant": compliantResult(), "findings": findingResult()} {
		var wrote bool
		mux := http.NewServeMux()
		mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		})
		write := func(w http.ResponseWriter, _ *http.Request) { wrote = true; w.WriteHeader(http.StatusOK) }
		mux.HandleFunc("POST /repos/o/r/issues", write)
		mux.HandleFunc("PATCH /repos/o/r/issues/{n}", write)
		mux.HandleFunc("POST /repos/o/r/labels", write)

		err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), result, "o", "r")
		if err == nil {
			t.Errorf("%s: Run returned nil after a failed issue search", name)
		}
		if wrote {
			t.Errorf("%s: wrote after a failed search (risks a duplicate issue)", name)
		}
	}
}

// Every write that fails is returned, so the runner can count it. Swallowing
// one of these is #104 again: a run that delivered nothing reads as green.
func TestRunReturnsWriteErrors(t *testing.T) {
	existing := `[{"number":42,"title":"[baseliner] baseline compliance findings"}]`
	cases := []struct {
		name   string
		search string
		result models.RepoResult
	}{
		{"create", `[]`, findingResult()},
		{"update", existing, findingResult()},
		{"close", existing, compliantResult()},
	}
	for _, tc := range cases {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tc.search))
		})
		mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"name":"baseliner"}`))
		})
		denied := func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
		}
		mux.HandleFunc("POST /repos/o/r/issues", denied)
		mux.HandleFunc("PATCH /repos/o/r/issues/42", denied)

		if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), tc.result, "o", "r"); err == nil {
			t.Errorf("%s: Run returned nil after the write was denied", tc.name)
		}
	}
}

// statefulIssue serves one findings issue whose body each write replaces, and
// records whether it was closed.
type statefulIssue struct {
	body   string
	closed bool
	writes int
}

func (si *statefulIssue) client(t *testing.T) *github.Client {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		if si.closed {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 42, "title": "[baseliner] baseline compliance findings", "body": si.body}})
	})
	mux.HandleFunc("PATCH /repos/o/r/issues/42", func(w http.ResponseWriter, r *http.Request) {
		var req github.IssueRequest
		decodeBody(t, r, &req)
		si.writes++
		si.body = req.GetBody()
		si.closed = req.GetState() == "closed"
		_, _ = w.Write([]byte(`{"number":42}`))
	})
	return fakeGitHub(t, mux)
}

func result(checks ...models.CheckResult) models.RepoResult {
	return models.RepoResult{Slug: "o/r", Score: models.ScorePtr(1.0), Results: checks}
}

func check(id string, st models.CheckStatus) models.CheckResult {
	return models.CheckResult{CheckID: id, Status: st, Severity: models.SeverityHigh, Message: sp(id + " message")}
}

// A check the issue lists as failing that cannot be read this run has not been
// shown fixed: the issue is updated with it still failing, not closed. A check
// listed as passing, or one that is skipped rather than unknown, does not hold
// the issue open.
func TestRunCarriesUnreadListedFinding(t *testing.T) {
	failRow := func(id string, st models.CheckStatus) string {
		return BuildBody(result(check(id, st)), time.Now())
	}
	for _, c := range []struct {
		name      string
		body      string
		now       models.CheckResult
		wantOpen  bool
		wantInRow string
	}{
		{"listed as failing", failRow("license_exists", models.StatusFail), check("license_exists", models.StatusUnknown), true, "last seen failing"},
		{"listed as errored", failRow("license_exists", models.StatusError), check("license_exists", models.StatusUnknown), true, "last seen failing"},
		{"listed as passing", failRow("license_exists", models.StatusPass), check("license_exists", models.StatusUnknown), false, ""},
		{"skipped, not unknown", failRow("license_exists", models.StatusFail), check("license_exists", models.StatusSkip), false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			si := &statefulIssue{body: c.body}
			if err := noWait(si.client(t), false).Run(context.Background(), result(c.now), "o", "r"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if si.closed == c.wantOpen {
				t.Errorf("closed = %v, want open = %v; body:\n%s", si.closed, c.wantOpen, si.body)
			}
			if c.wantOpen && !strings.Contains(si.body, "| `license_exists` | ❌ fail |") || !strings.Contains(si.body, c.wantInRow) {
				t.Errorf("body should keep license_exists as a failing row (%q):\n%s", c.wantInRow, si.body)
			}
		})
	}
}

// Across runs the carried finding survives the body rewrite: the issue closes
// only once the check is read and passes.
func TestRunKeepsCarriedFindingAcrossRewrites(t *testing.T) {
	si := &statefulIssue{body: BuildBody(result(check("license_exists", models.StatusFail), check("ci_present", models.StatusFail)), time.Now())}
	a := noWait(si.client(t), false)
	runs := []struct {
		r        models.RepoResult
		wantOpen bool
	}{
		{result(check("license_exists", models.StatusUnknown), check("ci_present", models.StatusFail)), true}, // body rewritten
		{result(check("license_exists", models.StatusUnknown), check("ci_present", models.StatusPass)), true}, // ci fixed, license still unread
		{result(check("license_exists", models.StatusPass), check("ci_present", models.StatusPass)), false},   // license read and fixed
	}
	for i, run := range runs {
		if err := a.Run(context.Background(), run.r, "o", "r"); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		if si.closed == run.wantOpen {
			t.Fatalf("run %d: closed = %v, want open = %v; body:\n%s", i+1, si.closed, run.wantOpen, si.body)
		}
	}
}

// A waived check appears in the findings issue with its reason, not as a
// finding.
func TestBuildBodyShowsWaiver(t *testing.T) {
	r := result(models.CheckResult{CheckID: "ci_present", Status: models.StatusWaived, Severity: models.SeverityHigh, Message: sp("waived by the repo: docs only")})
	if body := BuildBody(r, time.Now()); !strings.Contains(body, "| `ci_present` | 🔕 waived | high | waived by the repo: docs only |") {
		t.Errorf("body does not show the waiver:\n%s", body)
	}
	if hasFindings(r) {
		t.Error("a waived check is not a finding")
	}
}
