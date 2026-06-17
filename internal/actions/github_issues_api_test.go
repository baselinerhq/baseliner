package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func sampleResult() models.RepoResult {
	return models.RepoResult{
		Slug:  "o/r",
		Score: 1.0,
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

func TestRunCreatesIssueWhenNoneExists(t *testing.T) {
	var created, edited bool
	var labelsApplied []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"baseliner"}`)) // label already exists
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`)) // no existing findings issue
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

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), sampleResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !created || edited {
		t.Errorf("expected create only, got created=%v edited=%v", created, edited)
	}
	if len(labelsApplied) != 1 || labelsApplied[0] != "baseliner" {
		t.Errorf("expected baseliner label applied, got %v", labelsApplied)
	}
}

func TestRunUpdatesExistingIssue(t *testing.T) {
	var created, edited bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"baseliner"}`))
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		// An existing open issue with the exact findings title.
		_, _ = w.Write([]byte(`[{"number":42,"title":"[baseliner] baseline compliance findings"}]`))
	})
	mux.HandleFunc("POST /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":99}`))
	})
	mux.HandleFunc("PATCH /repos/o/r/issues/42", func(w http.ResponseWriter, _ *http.Request) {
		edited = true
		_, _ = w.Write([]byte(`{"number":42}`))
	})

	if err := noWait(fakeGitHub(t, mux), false).Run(context.Background(), sampleResult(), "o", "r"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if created || !edited {
		t.Errorf("expected edit only, got created=%v edited=%v", created, edited)
	}
}

func TestRunDryRunPerformsNoWrites(t *testing.T) {
	var wrote bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/labels/baseliner", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound) // label missing
	})
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	write := func(w http.ResponseWriter, _ *http.Request) { wrote = true; w.WriteHeader(http.StatusCreated) }
	mux.HandleFunc("POST /repos/o/r/labels", write)
	mux.HandleFunc("POST /repos/o/r/issues", write)
	mux.HandleFunc("PATCH /repos/o/r/issues/{n}", write)

	if err := noWait(fakeGitHub(t, mux), true).Run(context.Background(), sampleResult(), "o", "r"); err != nil {
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

	got := noWait(fakeGitHub(t, mux), false).ensureLabel(context.Background(), "o", "r")
	if !createdLabel {
		t.Error("expected label to be created when missing")
	}
	if got != "baseliner" {
		t.Errorf("ensureLabel returned %q, want baseliner", got)
	}
}

func TestFindExistingMatchesByTitleAcrossPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"number":5,"title":"[baseliner] baseline compliance findings"}]`))
			return
		}
		// Page 1: a non-matching issue + a Link header pointing to page 2.
		w.Header().Set("Link", `<`+r.URL.Path+`?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"number":1,"title":"something else"}]`))
	})

	got := noWait(fakeGitHub(t, mux), false).findExisting(context.Background(), "o", "r")
	if got == nil || got.GetNumber() != 5 {
		t.Fatalf("expected to find issue #5 on page 2, got %v", got)
	}
}
