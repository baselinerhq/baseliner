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
	existing := a.findExisting(ctx, owner, name)

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
		label := a.ensureLabel(ctx, owner, name)
		req := &github.IssueRequest{Title: github.Ptr(issueTitle), Body: github.Ptr(body)}
		if label != "" {
			req.Labels = &[]string{label}
		}
		issue, _, err := a.Client.Issues.Create(ctx, owner, name, req)
		if err != nil {
			return err
		}
		slog.Info("created issue", "number", issue.GetNumber(), "repo", result.Slug)
	}

	a.sleep(mutationSpacing)
	return nil
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

func (a GitHubIssues) ensureLabel(ctx context.Context, owner, name string) string {
	if _, _, err := a.Client.Issues.GetLabel(ctx, owner, name, issueLabel); err == nil {
		return issueLabel
	}
	if a.DryRun {
		slog.Debug("[dry-run] would create label", "label", issueLabel, "repo", owner+"/"+name)
		return ""
	}
	if _, _, err := a.Client.Issues.CreateLabel(ctx, owner, name, &github.Label{
		Name:        github.Ptr(issueLabel),
		Color:       github.Ptr(labelColor),
		Description: github.Ptr(labelDescription),
	}); err != nil {
		slog.Warn("could not create label", "repo", owner+"/"+name, "err", err)
		return ""
	}
	return issueLabel
}

func (a GitHubIssues) findExisting(ctx context.Context, owner, name string) *github.Issue {
	opt := &github.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{issueLabel},
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := a.Client.Issues.ListByRepo(ctx, owner, name, opt)
		if err != nil {
			slog.Warn("could not search issues", "repo", owner+"/"+name, "err", err)
			return nil
		}
		for _, is := range issues {
			if is.GetTitle() == issueTitle {
				return is
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil
}

var statusIcons = map[models.CheckStatus]string{
	models.StatusPass:  "✅",
	models.StatusFail:  "❌",
	models.StatusSkip:  "⏭️",
	models.StatusError: "⚠️",
}

// BuildBody renders the markdown issue body. Exported for golden testing.
func BuildBody(result models.RepoResult, now time.Time) string {
	timestamp := now.UTC().Format("2006-01-02 15:04 UTC")
	scorePct := fmt.Sprintf("%.0f%%", float64(result.Score)*100)

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
