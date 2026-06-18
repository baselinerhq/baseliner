package collectors

import (
	"context"
	"log/slog"
	"sort"
	"strings"
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
}

// NewGitHubAPI returns a collector with the default 90-day stale threshold.
func NewGitHubAPI(client *github.Client) GitHubAPI {
	return GitHubAPI{Client: client, StaleThresholdDays: defaultStaleThresholdDays, Now: time.Now}
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

	var files []string
	for _, p := range []string{"", ".github", ".github/workflows", ".circleci", "docs"} {
		files = append(files, c.listFiles(ctx, owner, name, p)...)
	}
	files = dedupeSort(files)

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

	return &models.NormalizedRepository{
		SourceType: models.SourceGitHub,
		Slug:       src.Slug,
		Name:       githubName(repo, src),
		FS: &models.FilesystemContext{
			Files:          files,
			KeyFiles:       DetectKeyFiles(files),
			ReadmeContent:  c.readme(ctx, owner, name),
			CIFiles:        DetectCIFiles(files),
			DepUpdateFiles: DetectDependencyUpdateFiles(files),
		},
		Git: &models.GitContext{
			DefaultBranch:   repo.DefaultBranch,
			LastCommitAt:    lastCommit,
			DaysSinceCommit: days,
			Branches:        c.branches(ctx, owner, name),
			IsStale:         isStale,
		},
	}
}

func (c GitHubAPI) listFiles(ctx context.Context, owner, name, p string) []string {
	_, dir, resp, err := c.Client.Repositories.GetContents(ctx, owner, name, p, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil
		}
		slog.Warn("github contents lookup failed", "path", p, "err", err)
		return nil
	}
	var out []string
	for _, item := range dir {
		if item.GetType() == "file" {
			out = append(out, item.GetPath())
		}
	}
	return out
}

func (c GitHubAPI) readme(ctx context.Context, owner, name string) *string {
	r, resp, err := c.Client.Repositories.GetReadme(ctx, owner, name, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil
		}
		slog.Warn("failed to fetch README", "err", err)
		return nil
	}
	content, err := r.GetContent()
	if err != nil {
		return nil
	}
	b := []byte(content)
	if len(b) > maxReadmeBytes {
		b = b[:maxReadmeBytes]
	}
	s := strings.ToValidUTF8(string(b), "�")
	return &s
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
