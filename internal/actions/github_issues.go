// Package actions performs side-effecting operations from scan results.
package actions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
)

const (
	issueTitle       = "[baseliner] baseline compliance findings"
	issueLabel       = "baseliner"
	labelColor       = "0075ca"
	labelDescription = "baseliner findings"
	mutationSpacing  = 1100 * time.Millisecond // GitHub recommends >=1s between writes
	resolvedBody     = "## baseliner findings\n\n✅ All baseline checks pass — closing this issue.\n\n---\n*managed by [baseliner](https://github.com/baselinerhq/baseliner)*"
)

// GitHubIssues opens or updates a single idempotent findings issue per repo.
type GitHubIssues struct {
	Client *github.Client
	DryRun bool
	Now    func() time.Time
	Sleep  func(time.Duration)
}

func (a GitHubIssues) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a GitHubIssues) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Run reconciles the findings issue for a repo: when the repo has findings it
// opens or updates the issue; when the repo is compliant it closes any open
// findings issue (and does nothing if there is none). This keeps issues to
// signal only — no "all green" noise on passing repos.
func (a GitHubIssues) Run(ctx context.Context, result models.RepoResult, owner, name string) error {
	existing, err := a.findExisting(ctx, owner, name)
	if err != nil {
		// Not "no issue": acting on that would skip a close or create a duplicate.
		return fmt.Errorf("search for an existing findings issue: %w", err)
	}

	if existing != nil {
		// A check the issue lists as failing that could not be read this run
		// has not been shown fixed, so it stays a finding in the issue, which
		// is therefore updated rather than closed.
		result = carryUnverified(result, existing.GetBody())
	}

	if !hasFindings(result) {
		if existing == nil {
			return nil // compliant and nothing to clean up
		}
		if a.DryRun {
			slog.Info("[dry-run] would close resolved issue", "number", existing.GetNumber(), "repo", result.Slug)
			return nil
		}
		if _, _, err := a.Client.Issues.Edit(ctx, owner, name, existing.GetNumber(),
			&github.IssueRequest{Body: github.Ptr(resolvedBody), State: github.Ptr("closed")}); err != nil {
			return err
		}
		slog.Info("closed resolved issue", "number", existing.GetNumber(), "repo", result.Slug)
		a.sleep(mutationSpacing)
		return nil
	}

	body := BuildBody(result, a.now())
	if existing != nil {
		if a.DryRun {
			slog.Info("[dry-run] would update issue", "number", existing.GetNumber(), "repo", result.Slug)
			return nil
		}
		if _, _, err := a.Client.Issues.Edit(ctx, owner, name, existing.GetNumber(),
			&github.IssueRequest{Body: github.Ptr(body)}); err != nil {
			return err
		}
		slog.Info("updated issue", "number", existing.GetNumber(), "repo", result.Slug)
	} else {
		if a.DryRun {
			slog.Info("[dry-run] would create issue", "repo", result.Slug)
			return nil
		}
		// The label is how findExisting finds this issue again, so an issue
		// opened without it would be duplicated on every later run.
		if err := a.ensureLabel(ctx, owner, name); err != nil {
			return fmt.Errorf("ensure the %q label: %w", issueLabel, err)
		}
		issue, _, err := a.Client.Issues.Create(ctx, owner, name, &github.IssueRequest{
			Title:  github.Ptr(issueTitle),
			Body:   github.Ptr(body),
			Labels: &[]string{issueLabel},
		})
		if err != nil {
			return err
		}
		slog.Info("created issue", "number", issue.GetNumber(), "repo", result.Slug)
	}

	a.sleep(mutationSpacing)
	return nil
}

// carryUnverified returns r with each check that is unknown in this run but
// listed as failing or errored in the issue body reported as failing, with a
// message saying so. Only the issue sees the result: its row stays a finding,
// so a later run that still cannot read the check keeps the issue open, and
// one that can read it updates or closes the issue on the evidence. Matching
// the body's own rows (see BuildBody) keeps a check that is permanently
// unknown but never failed, such as a plan-gated one, from holding an issue.
func carryUnverified(r models.RepoResult, body string) models.RepoResult {
	out := r
	out.Results = make([]models.CheckResult, len(r.Results))
	for i, c := range r.Results {
		if c.Status == models.StatusUnknown && listedAsFinding(c.CheckID, body) {
			msg := "last seen failing; could not be read this run"
			if c.Message != nil {
				msg += ": " + *c.Message
			}
			c.Status, c.Message = models.StatusFail, &msg
			slog.Info("keeping a finding the issue lists: the check could not be read",
				"repo", r.Slug, "check", c.CheckID)
		}
		out.Results[i] = c
	}
	return out
}

// listedAsFinding reports whether body has a failing or errored row for id.
func listedAsFinding(id, body string) bool {
	for _, st := range []models.CheckStatus{models.StatusFail, models.StatusError} {
		if strings.Contains(body, fmt.Sprintf("| `%s` | %s %s |", id, statusIcons[st], st)) {
			return true
		}
	}
	return false
}

// hasFindings reports whether a repo has any failing or errored check.
func hasFindings(r models.RepoResult) bool {
	for _, c := range r.Results {
		if c.Status == models.StatusFail || c.Status == models.StatusError {
			return true
		}
	}
	return false
}

func (a GitHubIssues) ensureLabel(ctx context.Context, owner, name string) error {
	if _, _, err := a.Client.Issues.GetLabel(ctx, owner, name, issueLabel); err == nil {
		return nil
	}
	_, _, err := a.Client.Issues.CreateLabel(ctx, owner, name, &github.Label{
		Name:        github.Ptr(issueLabel),
		Color:       github.Ptr(labelColor),
		Description: github.Ptr(labelDescription),
	})
	return err
}

func (a GitHubIssues) findExisting(ctx context.Context, owner, name string) (*github.Issue, error) {
	opt := &github.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{issueLabel},
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := a.Client.Issues.ListByRepo(ctx, owner, name, opt)
		if err != nil {
			return nil, err
		}
		for _, is := range issues {
			if is.GetTitle() == issueTitle {
				return is, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil, nil
}

var statusIcons = map[models.CheckStatus]string{
	models.StatusPass:    "✅",
	models.StatusFail:    "❌",
	models.StatusSkip:    "⏭️",
	models.StatusUnknown: "❔",
	models.StatusError:   "⚠️",
}

// BuildBody renders the markdown issue body. Exported for golden testing.
func BuildBody(result models.RepoResult, now time.Time) string {
	timestamp := now.UTC().Format("2006-01-02 15:04 UTC")
	scorePct := "n/a"
	if posture, ok := result.Posture(); ok {
		scorePct = fmt.Sprintf("%.0f%%", posture*100)
	}

	rows := make([]string, 0, len(result.Results)+2)
	rows = append(rows, "| check | status | severity | message |", "|---|---|---|---|")
	for _, c := range result.Results {
		icon, ok := statusIcons[c.Status]
		if !ok {
			icon = string(c.Status)
		}
		msg := ""
		if c.Message != nil {
			msg = *c.Message
		}
		rows = append(rows, fmt.Sprintf("| `%s` | %s %s | %s | %s |", c.CheckID, icon, c.Status, c.Severity, msg))
	}
	table := strings.Join(rows, "\n")

	return "## baseliner findings\n\n" +
		fmt.Sprintf("**Score**: %s  \n", scorePct) +
		fmt.Sprintf("**Scanned**: %s\n\n", timestamp) +
		table + "\n\n" +
		"---\n" +
		"*managed by [baseliner](https://github.com/baselinerhq/baseliner)*"
}
