// Package actions performs side-effecting operations from scan results.
package actions

import (
	"context"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
)

const mutationSpacing = 1100 * time.Millisecond // GitHub recommends >=1s between writes

// GitHubIssues opens or updates a single idempotent findings issue per repo.
type GitHubIssues struct {
	Client *github.Client
	DryRun bool
	Now    func() time.Time
	Sleep  func(time.Duration)
	// Login is the token's user, whose issues closed for a dropped label stop
	// another being opened. Empty when it is not known (an App token cannot
	// read it), and then nothing does.
	Login string
}

// Run reconciles the findings issue for the repo owner/name.
func (a GitHubIssues) Run(ctx context.Context, result models.RepoResult, owner, name string) error {
	r := reconciler{DryRun: a.DryRun, Now: a.Now, Sleep: a.Sleep, Spacing: mutationSpacing}
	return r.run(ctx, result, githubTracker{client: a.Client, owner: owner, name: name, login: a.Login})
}

// githubTracker is a GitHub repo's issues.
type githubTracker struct {
	client      *github.Client
	owner, name string
	login       string
}

func githubIssue(is *github.Issue) *Issue {
	out := &Issue{Number: is.GetNumber(), Body: is.GetBody()}
	for _, l := range is.Labels {
		out.Labels = append(out.Labels, l.GetName())
	}
	return out
}

func (t githubTracker) FindOpen(ctx context.Context) (*Issue, error) {
	opt := &github.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{issueLabel},
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := t.client.Issues.ListByRepo(ctx, t.owner, t.name, opt)
		if err != nil {
			return nil, err
		}
		for _, is := range issues {
			if is.GetTitle() == issueTitle {
				return githubIssue(is), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil, nil
}

// FindDropped returns the most recent findings issue the token's user opened
// and closed because its label was dropped, or nil. Only that user's issues
// count, so nobody else can block the findings issue by opening and closing
// one that looks the same; it reads the latest 100 of them.
func (t githubTracker) FindDropped(ctx context.Context) (*Issue, error) {
	if t.login == "" {
		return nil, nil
	}
	issues, _, err := t.client.Issues.ListByRepo(ctx, t.owner, t.name, &github.IssueListByRepoOptions{
		State: "closed", Creator: t.login, Sort: "created", Direction: "desc", ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, err
	}
	for _, is := range issues {
		if is.GetTitle() == issueTitle && strings.HasPrefix(is.GetBody(), droppedMarker) {
			return githubIssue(is), nil
		}
	}
	return nil, nil
}

func (t githubTracker) EnsureLabel(ctx context.Context) (Label, error) {
	if _, _, err := t.client.Issues.GetLabel(ctx, t.owner, t.name, issueLabel); err == nil {
		return Label{Name: issueLabel}, nil
	}
	_, _, err := t.client.Issues.CreateLabel(ctx, t.owner, t.name, &github.Label{
		Name:        github.Ptr(issueLabel),
		Color:       github.Ptr(labelColor),
		Description: github.Ptr(labelDescription),
	})
	return Label{Name: issueLabel}, err
}

func (t githubTracker) Create(ctx context.Context, body string, label Label) (*Issue, error) {
	is, _, err := t.client.Issues.Create(ctx, t.owner, t.name, &github.IssueRequest{
		Title:  github.Ptr(issueTitle),
		Body:   github.Ptr(body),
		Labels: &[]string{label.Name},
	})
	if err != nil {
		return nil, err
	}
	return githubIssue(is), nil
}

func (t githubTracker) Update(ctx context.Context, number int, body string) error {
	_, _, err := t.client.Issues.Edit(ctx, t.owner, t.name, number, &github.IssueRequest{Body: github.Ptr(body)})
	return err
}

func (t githubTracker) Close(ctx context.Context, number int, body string) error {
	_, _, err := t.client.Issues.Edit(ctx, t.owner, t.name, number,
		&github.IssueRequest{Body: github.Ptr(body), State: github.Ptr("closed")})
	return err
}

// Labelled: GitHub matches label names without regard to case.
func (githubTracker) Labelled(is *Issue) bool { return hasLabelFold(is.Labels, issueLabel) }

func (githubTracker) Forge() string        { return "GitHub" }
func (githubTracker) AccessNeeded() string { return "push access" }
