package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baselinerhq/baseliner/internal/models"
)

// fakeTracker records what the reconciler asks of a forge.
type fakeTracker struct {
	open, dropped *Issue
	findErr       error
	labelErr      error
	createLabels  []string // labels the forge keeps on a new issue
	calls         []string
}

func (f *fakeTracker) FindOpen(context.Context) (*Issue, error) {
	f.calls = append(f.calls, "find")
	return f.open, f.findErr
}

func (f *fakeTracker) FindDropped(context.Context) (*Issue, error) {
	f.calls = append(f.calls, "find-dropped")
	return f.dropped, nil
}

func (f *fakeTracker) EnsureLabel(context.Context) (Label, error) {
	f.calls = append(f.calls, "label")
	return Label{Name: issueLabel, ID: 7}, f.labelErr
}

func (f *fakeTracker) Create(_ context.Context, body string, label Label) (*Issue, error) {
	f.calls = append(f.calls, fmt.Sprintf("create %s %d", label.Name, label.ID))
	return &Issue{Number: 9, Body: body, Labels: f.createLabels}, nil
}

func (f *fakeTracker) Update(_ context.Context, n int, _ string) error {
	f.calls = append(f.calls, "update")
	return nil
}

func (f *fakeTracker) Close(_ context.Context, n int, body string) error {
	head, _, _ := strings.Cut(body, "\n")
	f.calls = append(f.calls, "close "+head)
	return nil
}

func (*fakeTracker) Forge() string        { return "Forge" }
func (*fakeTracker) AccessNeeded() string { return "some access" }

func contractResult(status models.CheckStatus) models.RepoResult {
	return models.RepoResult{Slug: "o/r", Score: models.ScorePtr(0.5), Results: []models.CheckResult{
		{CheckID: "license_exists", Status: status, Severity: models.SeverityHigh},
	}}
}

// The rules every forge's findings issues follow, run against a fake
// tracker so a new adapter inherits them: one issue per repo, opened with
// its label or closed at once without it, updated on findings, closed only
// on evidence, never on a check that could not be read, and no write in a
// dry run or after a failed search.
func TestReconcileContract(t *testing.T) {
	listed := &Issue{Number: 3, Body: BuildBody(contractResult(models.StatusFail), time.Unix(0, 0)), Labels: []string{issueLabel}}
	for _, c := range []struct {
		name    string
		tracker fakeTracker
		status  models.CheckStatus
		dryRun  bool
		calls   string
		errHas  string
	}{
		{"compliant, nothing open", fakeTracker{}, models.StatusPass, false, "find", ""},
		{"compliant, issue open", fakeTracker{open: listed}, models.StatusPass, false, "find|close ## baseliner findings", ""},
		{"unread check the issue lists", fakeTracker{open: listed}, models.StatusUnknown, false, "find|update", ""},
		{"findings, issue open", fakeTracker{open: listed}, models.StatusFail, false, "find|update", ""},
		{"findings, none open", fakeTracker{createLabels: []string{"BaseLiner"}}, models.StatusFail, false, "find|find-dropped|label|create baseliner 7", ""},
		{"label dropped on create", fakeTracker{}, models.StatusFail, false, "find|find-dropped|label|create baseliner 7|close " + droppedMarker + " Forge dropped",
			`issue #9 was created without its label and closed: Forge dropped the "baseliner" label, which it does when the token's user lacks some access to the repo`},
		{"dropped before", fakeTracker{dropped: &Issue{Number: 4}}, models.StatusFail, false, "find|find-dropped",
			`not creating a findings issue: Forge dropped the "baseliner" label from #4, which it does when the token's user lacks some access to the repo; give it some access, then delete or reopen and label #4`},
		{"label cannot be ensured", fakeTracker{labelErr: errors.New("403")}, models.StatusFail, false, "find|find-dropped|label", "ensure the \"baseliner\" label: 403"},
		{"search failed", fakeTracker{findErr: errors.New("500")}, models.StatusPass, false, "find", "search for an existing findings issue: 500"},
		{"dry run, would create", fakeTracker{}, models.StatusFail, true, "find|find-dropped", ""},
		{"dry run, would close", fakeTracker{open: listed}, models.StatusPass, true, "find", ""},
		{"dry run, would update", fakeTracker{open: listed}, models.StatusFail, true, "find", ""},
	} {
		tr := c.tracker
		r := reconciler{DryRun: c.dryRun, Now: func() time.Time { return time.Unix(0, 0) }, Sleep: func(time.Duration) {}}
		err := r.run(context.Background(), contractResult(c.status), &tr)
		if got := strings.Join(tr.calls, "|"); !strings.HasPrefix(got, c.calls) || len(strings.Split(got, "|")) != len(strings.Split(c.calls, "|")) {
			t.Errorf("%s: calls %q, want %q", c.name, got, c.calls)
		}
		switch {
		case c.errHas == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.errHas != "" && (err == nil || err.Error() != c.errHas && !strings.HasPrefix(err.Error(), c.errHas)):
			t.Errorf("%s: error %v, want %q", c.name, err, c.errHas)
		}
	}
}
