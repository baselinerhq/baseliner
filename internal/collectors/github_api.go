package collectors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
	"github.com/baselinerhq/baseliner/internal/waivers"
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

	// Observe, when set, is passed every error an API call returned, so the
	// caller can tell when the scan was rate-limited.
	Observe func(error)

	// ExtraDirs are directories to list beyond evidenceDirs, for a policy's
	// file_present checks.
	ExtraDirs []string
}

// Visibility returns repo's visibility as GitHub reports it: public, private
// or internal. private: true wins over a missing or contradicting "public",
// and a repo with neither is public.
func Visibility(repo *github.Repository) string {
	v := strings.ToLower(repo.GetVisibility())
	switch {
	case repo.GetPrivate() && (v == "" || v == "public"):
		return "private"
	case v == "":
		return "public"
	}
	return v
}

// observe passes a failed API call's error to Observe.
func (c GitHubAPI) observe(err error) {
	if c.Observe != nil && err != nil {
		c.Observe(err)
	}
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

	// A read that failed for any reason but 404 is recorded rather than read
	// as absence, so the checks that depend on it report unknown instead of
	// failing as if the files were missing.
	var files, unread []string
	for _, p := range withExtra(evidenceDirs, c.ExtraDirs) {
		got, ok := c.listFiles(ctx, owner, name, p)
		files = append(files, got...)
		if !ok {
			unread = append(unread, p)
		}
	}
	files = dedupeSort(files)
	ciFiles := DetectCIFiles(files)
	readme, readmeOK := c.readme(ctx, owner, name)

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

	return &models.NormalizedRepository{
		SourceType: models.SourceGitHub,
		Slug:       src.Slug,
		Name:       githubName(repo, src),
		Platform:   platform,
		Visibility: Visibility(repo),
		Waivers:    c.waivers(ctx, owner, name, files, src.Slug),
		FS: &models.FilesystemContext{
			Files:           files,
			KeyFiles:        DetectKeyFiles(files),
			ReadmeContent:   readme,
			CIFiles:         ciFiles,
			InactiveCIFiles: c.inactiveWorkflows(ctx, owner, name, repo.GetFork(), ciFiles),
			DepUpdateFiles:  DetectDependencyUpdateFiles(files),
			UnreadDirs:      unread,
			ReadmeUnread:    !readmeOK,
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
			c.observe(err)
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

// waivers reads the repo's waiver file when the root listing has one, from
// the default branch, as its git blob. The contents API reports a symlink as a
// file and serves its target, so reading the blob by the SHA it gives yields a
// symlink's own text, which does not parse, as a local scan refuses one. A
// file that cannot be read or parsed declares no waivers: the checks run as
// they would without it, and a warning says why.
func (c GitHubAPI) waivers(ctx context.Context, owner, name string, files []string, slug string) []models.Waiver {
	file := waiverFile(files, slug)
	if file == "" {
		return nil
	}
	f, _, _, err := c.Client.Repositories.GetContents(ctx, owner, name, file, nil)
	if err != nil {
		c.observe(err)
		slog.Warn("ignoring repo waivers: file could not be read", "repo", slug, "file", file)
		return nil
	}
	if f == nil || f.GetType() != "file" || f.GetSHA() == "" {
		slog.Warn("ignoring repo waivers: not a regular file", "repo", slug, "file", file)
		return nil
	}
	if f.GetSize() > waivers.MaxBytes {
		slog.Warn("ignoring repo waivers: file is over the size limit", "repo", slug, "file", file)
		return nil
	}
	data, _, err := c.Client.Git.GetBlobRaw(ctx, owner, name, f.GetSHA())
	if err != nil {
		c.observe(err)
		slog.Warn("ignoring repo waivers: file could not be read", "repo", slug, "file", file)
		return nil
	}
	ws, err := waivers.Parse(data)
	if err != nil {
		slog.Warn("ignoring repo waivers", "repo", slug, "file", file, "err", err)
		return nil
	}
	return ws
}

// rawReadme fetches the README's raw bytes, for one too large for the contents
// API to inline. Only the first maxReadmeBytes are kept.
func (c GitHubAPI) rawReadme(ctx context.Context, owner, name string) (string, bool) {
	req, err := c.Client.NewRequest("GET", fmt.Sprintf("repos/%s/%s/readme", owner, name), nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	var buf bytes.Buffer
	if _, err := c.Client.Do(ctx, req, &limitedWriter{w: &buf, n: maxReadmeBytes}); err != nil && !errors.Is(err, errLimitReached) {
		c.observe(err)
		slog.Warn("failed to fetch raw README", "err", err)
		return "", false
	}
	return buf.String(), true
}

// errLimitReached stops a copy into a limitedWriter once it holds n bytes, so
// the rest of a large body is not downloaded.
var errLimitReached = errors.New("limit reached")

// limitedWriter keeps the first n bytes written to it, then reports
// errLimitReached.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	keep := min(len(p), l.n)
	if _, err := l.w.Write(p[:keep]); err != nil {
		return 0, err
	}
	l.n -= keep
	if keep < len(p) || l.n == 0 {
		return keep, errLimitReached
	}
	return keep, nil
}

// listFiles returns the files directly under p, none if p does not exist
// (404), and false if it could not be read.
func (c GitHubAPI) listFiles(ctx context.Context, owner, name, p string) ([]string, bool) {
	_, dir, resp, err := c.Client.Repositories.GetContents(ctx, owner, name, p, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, true
		}
		c.observe(err)
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
		c.observe(err)
		slog.Warn("failed to fetch README", "err", err)
		return nil, false
	}
	var content string
	if r.GetEncoding() == "none" {
		// Over 1 MB the API sends no content; fetch the README raw instead.
		raw, ok := c.rawReadme(ctx, owner, name)
		if !ok {
			return nil, false
		}
		content = raw
	} else {
		content, err = r.GetContent()
		if err != nil {
			slog.Warn("failed to decode README", "err", err)
			return nil, false
		}
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
			c.observe(err)
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
