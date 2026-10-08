package collectors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

// GitHubAPI collects a NormalizedRepository (fs + git context) from the GitHub API.
type GitHubAPI struct {
	Client             *github.Client
	StaleThresholdDays int
	Now                func() time.Time
	// Platform also collects the platform layer (branch protection and
	// rulesets on the default branch). Off unless a platform check is enabled,
	// because it costs at least two more API calls per repo.
	Platform bool

	// fallbackWarned makes the ci_present fallback warning once per run.
	fallbackWarned *sync.Once
}

// NewGitHubAPI returns a collector with the default 90-day stale threshold.
func NewGitHubAPI(client *github.Client) GitHubAPI {
	return GitHubAPI{Client: client, StaleThresholdDays: defaultStaleThresholdDays, Now: time.Now,
		fallbackWarned: &sync.Once{}}
}

func (c GitHubAPI) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Collect fetches a shallow file listing (root, .github, .github/workflows,
// .circleci, docs), README, branches (cap 100), default branch, and
// pushed_at-based staleness. The docs/ listing keeps CODEOWNERS detection at
// parity with the local walk (GitHub recognizes CODEOWNERS in root, .github, or
// docs).
func (c GitHubAPI) Collect(ctx context.Context, src source.Repo) *models.NormalizedRepository {
	repo, _ := src.GitHubRepo.(*github.Repository)
	if repo == nil {
		return emptyGitHubResult(src)
	}
	owner := repo.GetOwner().GetLogin()
	name := repo.GetName()

	// A read that failed for any reason but 404 leaves the filesystem view
	// unavailable: its checks then report unknown, where a partial listing
	// would fail them as if the files were missing.
	var files []string
	readable := true
	for _, p := range []string{"", ".github", ".github/workflows", ".circleci", "docs"} {
		got, ok := c.listFiles(ctx, owner, name, p)
		files = append(files, got...)
		readable = readable && ok
	}
	files = dedupeSort(files)
	ciFiles := DetectCIFiles(files)
	readme, ok := c.readme(ctx, owner, name)
	readable = readable && ok

	var lastCommit *time.Time
	var days *int
	isStale := false
	if repo.PushedAt != nil {
		t := repo.GetPushedAt().UTC()
		lastCommit = &t
		d := int(c.now().Sub(t).Hours() / 24)
		days = &d
		isStale = d > c.StaleThresholdDays
	}

	var platform *models.PlatformContext
	if c.Platform && repo.GetDefaultBranch() != "" {
		platform = c.collectPlatform(ctx, owner, name, repo.GetDefaultBranch())
	}

	var fs *models.FilesystemContext
	if readable {
		fs = &models.FilesystemContext{
			Files:           files,
			KeyFiles:        DetectKeyFiles(files),
			ReadmeContent:   readme,
			CIFiles:         ciFiles,
			InactiveCIFiles: c.inactiveWorkflows(ctx, owner, name, repo.GetFork(), ciFiles),
			DepUpdateFiles:  DetectDependencyUpdateFiles(files),
		}
	}

	return &models.NormalizedRepository{
		SourceType: models.SourceGitHub,
		Slug:       src.Slug,
		Name:       githubName(repo, src),
		Platform:   platform,
		FS:         fs,
		Git: &models.GitContext{
			DefaultBranch:   repo.DefaultBranch,
			LastCommitAt:    lastCommit,
			DaysSinceCommit: days,
			Branches:        c.branches(ctx, owner, name),
			IsStale:         isStale,
		},
	}
}

// maxWorkflowPages bounds the Actions workflows listing (100 per page).
const maxWorkflowPages = 10

// notListed is the InactiveCIFiles reason for a fork's workflow file that
// GitHub's complete listing does not include.
const notListed = "not listed by GitHub Actions"

// inactiveWorkflows maps each GitHub Actions workflow file that is not running
// to why: the state GitHub reports when it is not active, or, on a fork,
// notListed when GitHub does not list it (a fork whose Actions were never
// enabled lists nothing). On a non-fork an unlisted file counts as running:
// GitHub registers a workflow only once an event or a push to the file
// reaches it, so a valid workflow that has never triggered is unlisted too.
//
// It returns nil when the state is unknown, so ci_present falls back to file
// presence: the repo has no workflow files, or the listing could not be read
// in full (e.g. a token without Actions read access, or more pages than
// maxWorkflowPages). A partial listing is never used, because on a fork files
// on unread pages would fail falsely as unlisted.
func (c GitHubAPI) inactiveWorkflows(ctx context.Context, owner, name string, fork bool, ciFiles []string) map[string]string {
	unlisted := "active"
	if fork {
		unlisted = notListed
	}
	state := map[string]string{}
	for _, f := range ciFiles {
		if strings.HasPrefix(f, ".github/workflows/") {
			state[f] = unlisted
		}
	}
	if len(state) == 0 {
		return nil
	}
	opt := &github.ListOptions{PerPage: 100}
	for range maxWorkflowPages {
		page, resp, err := c.Client.Actions.ListWorkflows(ctx, owner, name, opt)
		if err != nil {
			c.warnFallback(err)
			return nil
		}
		for _, w := range page.Workflows {
			if _, ok := state[w.GetPath()]; ok {
				state[w.GetPath()] = w.GetState()
			}
		}
		if resp.NextPage == 0 {
			for f, s := range state {
				if s == "active" {
					delete(state, f)
				}
			}
			return state
		}
		opt.Page = resp.NextPage
	}
	c.warnFallback(fmt.Errorf("more than %d pages of workflows", maxWorkflowPages))
	return nil
}

// warnFallback logs, once per run, that ci_present is falling back to file
// presence, which reads as a pass for disabled workflows. The warning is about
// every repo's results but fires for whichever repo fails first, so it carries
// only the HTTP status: an error quoting the API URL names that repo, and in
// exclude mode the privacy guard would drop the warning with it. The full
// error goes to the debug log per repo.
func (c GitHubAPI) warnFallback(err error) {
	if c.fallbackWarned == nil {
		return
	}
	slog.Debug("workflow listing failed", "err", err)
	reason := "listing incomplete"
	var er *github.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		reason = fmt.Sprintf("HTTP %d", er.Response.StatusCode)
	}
	c.fallbackWarned.Do(func() {
		slog.Warn("workflow state not read in full; ci_present falls back to file presence "+
			"and passes disabled workflows (a token without Actions: Read is the usual cause)", "reason", reason)
	})
}

// listFiles returns the files directly under p, none if p does not exist
// (404), and false if it could not be read.
func (c GitHubAPI) listFiles(ctx context.Context, owner, name, p string) ([]string, bool) {
	_, dir, resp, err := c.Client.Repositories.GetContents(ctx, owner, name, p, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, true
		}
		slog.Warn("github contents lookup failed", "path", p, "err", err)
		return nil, false
	}
	var out []string
	for _, item := range dir {
		if item.GetType() == "file" {
			out = append(out, item.GetPath())
		}
	}
	return out, true
}

// readme returns the README's content, nil if there is none (404), and false
// if it could not be read.
func (c GitHubAPI) readme(ctx context.Context, owner, name string) (*string, bool) {
	r, resp, err := c.Client.Repositories.GetReadme(ctx, owner, name, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, true
		}
		slog.Warn("failed to fetch README", "err", err)
		return nil, false
	}
	content, err := r.GetContent()
	if err != nil {
		slog.Warn("failed to decode README", "err", err)
		return nil, false
	}
	b := []byte(content)
	if len(b) > maxReadmeBytes {
		b = b[:maxReadmeBytes]
	}
	s := strings.ToValidUTF8(string(b), "�")
	return &s, true
}

func (c GitHubAPI) branches(ctx context.Context, owner, name string) []string {
	var names []string
	opt := &github.BranchListOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		page, resp, err := c.Client.Repositories.ListBranches(ctx, owner, name, opt)
		if err != nil {
			slog.Warn("failed to list branches", "err", err)
			break
		}
		for _, b := range page {
			if len(names) >= 100 {
				return names
			}
			names = append(names, b.GetName())
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return names
}

func dedupeSort(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func githubName(repo *github.Repository, src source.Repo) string {
	if n := repo.GetName(); n != "" {
		return n
	}
	return resolveName(src)
}

func emptyGitHubResult(src source.Repo) *models.NormalizedRepository {
	return &models.NormalizedRepository{
		SourceType: models.SourceGitHub,
		Slug:       src.Slug,
		Name:       resolveName(src),
		FS: &models.FilesystemContext{
			Files: []string{}, KeyFiles: emptyKeyFiles(), ReadmeContent: nil,
			CIFiles: []string{}, DepUpdateFiles: []string{},
		},
		Git: &models.GitContext{
			DefaultBranch: nil, LastCommitAt: nil, DaysSinceCommit: nil,
			Branches: []string{}, IsStale: false,
		},
	}
}
