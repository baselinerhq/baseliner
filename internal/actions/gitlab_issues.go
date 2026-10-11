package actions

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
)

// maxIssuePages bounds the listing of a project's open findings issues (100
// per page). A listing cut short cannot show there is no issue, so it is an
// error rather than "none".
const maxIssuePages = 10

// GitLabIssues opens or updates a single findings issue per GitLab project,
// with the token --open-issues writes with.
type GitLabIssues struct {
	Client *gitlab.Client
	DryRun bool
	Now    func() time.Time
	Sleep  func(time.Duration)
	// Username is the token's user, whose issues closed for a dropped label
	// stop another being opened. Empty when it is not known, and then
	// nothing does.
	Username string
}

// Run reconciles the findings issue for the project with projectID.
func (a GitLabIssues) Run(ctx context.Context, result models.RepoResult, projectID int64) error {
	r := reconciler{DryRun: a.DryRun, Now: a.Now, Sleep: a.Sleep, Spacing: mutationSpacing}
	return r.run(ctx, result, gitlabTracker{client: a.Client, project: projectID, username: a.Username})
}

// gitlabTracker is a GitLab project's issues; an issue's number is its IID.
type gitlabTracker struct {
	client   *gitlab.Client
	project  int64
	username string
}

func gitlabIssue(is gitlab.Issue) *Issue {
	return &Issue{Number: is.IID, Body: is.Description, Labels: is.Labels}
}

func (t gitlabTracker) FindOpen(ctx context.Context) (*Issue, error) {
	issues, complete, err := t.client.ProjectIssues(ctx, t.project,
		url.Values{"labels": {issueLabel}, "state": {"opened"}}, maxIssuePages)
	if err != nil {
		return nil, err
	}
	for _, is := range issues {
		if is.Title == issueTitle {
			return gitlabIssue(is), nil
		}
	}
	if !complete {
		return nil, fmt.Errorf("more than %d pages of issues labelled %q", maxIssuePages, issueLabel)
	}
	return nil, nil
}

// FindDropped returns the most recent findings issue the token's user opened
// and closed because its label was dropped, or nil. Only that user's issues
// count, so nobody else can block the findings issue by opening and closing
// one that looks the same; it reads the latest 100 of them.
func (t gitlabTracker) FindDropped(ctx context.Context) (*Issue, error) {
	if t.username == "" {
		return nil, nil
	}
	issues, _, err := t.client.ProjectIssues(ctx, t.project, url.Values{
		"state": {"closed"}, "author_username": {t.username}, "order_by": {"created_at"}, "sort": {"desc"},
	}, 1)
	if err != nil {
		return nil, err
	}
	for _, is := range issues {
		if is.Title == issueTitle && strings.HasPrefix(is.Description, droppedMarker) {
			return gitlabIssue(is), nil
		}
	}
	return nil, nil
}

func (t gitlabTracker) EnsureLabel(ctx context.Context) (Label, error) {
	ok, err := t.client.LabelExists(ctx, t.project, issueLabel)
	if err == nil && !ok {
		err = t.client.CreateLabel(ctx, t.project, issueLabel, "#"+labelColor, labelDescription)
	}
	return Label{Name: issueLabel}, err
}

func (t gitlabTracker) Create(ctx context.Context, body string, label Label) (*Issue, error) {
	is, err := t.client.CreateIssue(ctx, t.project, issueTitle, body, []string{label.Name})
	if err != nil {
		return nil, err
	}
	return gitlabIssue(is), nil
}

func (t gitlabTracker) Update(ctx context.Context, number int, body string) error {
	return t.client.EditIssue(ctx, t.project, number, body, false)
}

func (t gitlabTracker) Close(ctx context.Context, number int, body string) error {
	return t.client.EditIssue(ctx, t.project, number, body, true)
}

// Labelled: GitLab's label names are case-sensitive, so "Baseliner" is
// another label, which the listing that finds the issue would not match.
func (gitlabTracker) Labelled(is *Issue) bool { return slices.Contains(is.Labels, issueLabel) }

func (gitlabTracker) Forge() string { return "GitLab" }

// AccessNeeded: on GitLab 19.4 CE a Guest cannot create the label, and a
// new issue of theirs naming a label that did not exist lost it; once the
// label existed, a Guest's issue kept it. Planner and above create it.
func (gitlabTracker) AccessNeeded() string { return "Planner (or higher) access" }
