package collectors

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
	"github.com/baselinerhq/baseliner/internal/waivers"
)

// maxTreePages bounds each directory listing (100 entries a page).
const maxTreePages = 10

// gitlabEvidenceDirs are the directories listed for a GitLab project: the
// same as for GitHub, and .gitlab/, where GitLab also reads CODEOWNERS.
var gitlabEvidenceDirs = append(slices.Clone(evidenceDirs), ".gitlab")

// GitLabAPI collects a GitLab project's evidence through the API: the same
// directory listings as for GitHub, the README, the waiver file, branches and
// last activity. It reads no platform evidence, so the platform checks report
// unknown, and no CI state, so ci_present uses file presence.
type GitLabAPI struct {
	Client             *gitlab.Client
	StaleThresholdDays int
	Now                func() time.Time
	// Observe, when set, is passed each API error, as for GitHub.
	Observe func(error)
}

// NewGitLabAPI returns a collector with the default 90-day stale threshold.
func NewGitLabAPI(c *gitlab.Client) GitLabAPI {
	return GitLabAPI{Client: c, StaleThresholdDays: defaultStaleThresholdDays, Now: time.Now}
}

func (c GitLabAPI) observe(err error) {
	if c.Observe != nil {
		c.Observe(err)
	}
}

// Collect reads the project behind src. A read that failed for any reason but
// a 404 is recorded as unread, not as absence, so the checks that depend on it
// report unknown. Every warning carries the slug, so the privacy guard can
// mask or drop it.
func (c GitLabAPI) Collect(ctx context.Context, src source.Repo) *models.NormalizedRepository {
	p, _ := src.ForgeRepo.(*gitlab.Project)
	if p == nil {
		return emptyResult(src)
	}
	ref := p.DefaultBranch
	var files, unread, ciFiles []string
	var readme *string
	readmeOK := true
	var branches []string
	switch {
	case p.EmptyRepo:
		// Nothing to read: every file is genuinely absent.
	case ref == "":
		// GitLab leaves the default branch out when the token cannot read the
		// repository (a Guest on a private project, or repository access
		// limited): nothing could be read, which is not absence.
		unread = slices.Clone(gitlabEvidenceDirs)
		readmeOK = false
		slog.Warn("gitlab repository not readable: no default branch reported", "repo", src.Slug)
	default:
		for _, dir := range gitlabEvidenceDirs {
			got, ok := c.listFiles(ctx, p.ID, ref, dir, src.Slug)
			files = append(files, got...)
			if !ok {
				unread = append(unread, dir)
			}
		}
		files = dedupeSort(files)
		ciFiles, unread = c.ciFiles(ctx, p, files, unread, src.Slug)
		readme, readmeOK = c.readme(ctx, p.ID, ref, files, src.Slug)
		branches = c.branches(ctx, p.ID, src.Slug)
	}
	if files == nil {
		files = []string{}
	}
	if branches == nil {
		branches = []string{}
	}

	var lastCommit *time.Time
	var days *int
	isStale := false
	if p.LastActivityAt != nil {
		t := p.LastActivityAt.UTC()
		lastCommit = &t
		d := int(c.Now().Sub(t).Hours() / 24)
		days = &d
		isStale = d > c.StaleThresholdDays
	}
	var defaultBranch *string
	if ref != "" {
		defaultBranch = &ref
	}

	return &models.NormalizedRepository{
		SourceType: models.SourceGitLab,
		Slug:       src.Slug,
		Name:       p.Path,
		Visibility: src.Visibility,
		Waivers:    c.waivers(ctx, p.ID, ref, files, src.Slug),
		FS: &models.FilesystemContext{
			Files:          files,
			KeyFiles:       DetectKeyFiles(files),
			ReadmeContent:  readme,
			CIFiles:        ciFiles,
			DepUpdateFiles: DetectDependencyUpdateFiles(files),
			UnreadDirs:     unread,
			ReadmeUnread:   !readmeOK,
		},
		Git: &models.GitContext{
			DefaultBranch:   defaultBranch,
			LastCommitAt:    lastCommit,
			DaysSinceCommit: days,
			Branches:        branches,
			IsStale:         isStale,
		},
	}
}

// externalCI stands in CIFiles for CI configured in another project or at a
// URL, which is not named: the other project may be private.
const externalCI = "(CI configuration outside the repository)"

// ciFiles returns the project's CI files, and unread with the custom CI
// path's directory added when that file could not be read. With a custom CI
// configuration path GitLab ignores a root .gitlab-ci.yml, so only that path
// counts: in the repo when it exists, and in another project or at a URL as
// configured CI.
func (c GitLabAPI) ciFiles(ctx context.Context, p *gitlab.Project, files, unread []string, slug string) ([]string, []string) {
	custom := strings.TrimSpace(p.CIConfigPath)
	if custom == "" || custom == ".gitlab-ci.yml" {
		return DetectCIFiles(files), unread
	}
	var ci []string
	for _, f := range DetectCIFiles(files) {
		if f != ".gitlab-ci.yml" {
			ci = append(ci, f)
		}
	}
	if strings.Contains(custom, "@") || strings.Contains(custom, "://") {
		return append(ci, externalCI), unread
	}
	_, err := c.Client.RawFile(ctx, p.ID, p.DefaultBranch, custom, 1)
	switch {
	case err == nil:
		ci = append(ci, custom)
	case !gitlab.IsAbsent(err):
		c.observe(err)
		slog.Warn("could not read the custom CI configuration", "repo", slug, "err", err)
		dir := path.Dir(custom)
		if dir == "." {
			dir = ""
		}
		unread = append(unread, dir)
	}
	return ci, unread
}

// listFiles returns the files directly under dir, none when it does not
// exist, and false when it could not be read in full. Only blobs count:
// a symlink (mode 120000) or a submodule is not a file in the repo, as the
// GitHub listing does not count them either.
func (c GitLabAPI) listFiles(ctx context.Context, id int64, ref, dir, slug string) ([]string, bool) {
	entries, complete, err := c.Client.Tree(ctx, id, ref, dir, maxTreePages)
	if err != nil {
		if gitlab.IsAbsent(err) {
			return nil, true
		}
		c.observe(err)
		slog.Warn("gitlab tree listing failed", "repo", slug, "dir", dir, "err", err)
		return nil, false
	}
	if !complete {
		slog.Warn("gitlab tree listing incomplete", "repo", slug, "dir", dir)
		return nil, false
	}
	var out []string
	for _, e := range entries {
		if e.Type == "blob" && e.Mode != "120000" {
			out = append(out, e.Path)
		}
	}
	return out, true
}

// readme returns the listed README's first maxReadmeBytes as UTF-8, nil when
// none is listed, and false when it is listed but could not be read.
func (c GitLabAPI) readme(ctx context.Context, id int64, ref string, files []string, slug string) (*string, bool) {
	rel := FindReadmePath(files)
	if rel == "" {
		return nil, true
	}
	data, err := c.Client.RawFile(ctx, id, ref, rel, maxReadmeBytes)
	if err != nil {
		c.observe(err)
		slog.Warn("could not read README", "repo", slug, "err", err)
		return nil, false
	}
	s := strings.ToValidUTF8(string(data), "�")
	return &s, true
}

// waivers reads the waiver file when the root listing has one. A file that
// cannot be read, is over the size limit or does not parse declares no
// waivers: the checks run, and a warning says why.
func (c GitLabAPI) waivers(ctx context.Context, id int64, ref string, files []string, slug string) []models.Waiver {
	file := waiverFile(files, slug)
	if file == "" {
		return nil
	}
	data, err := c.Client.RawFile(ctx, id, ref, file, waivers.MaxBytes+1)
	if err != nil {
		c.observe(err)
		slog.Warn("ignoring repo waivers: file could not be read", "repo", slug, "file", file)
		return nil
	}
	// Parse rejects a file over the limit, and the read stops one byte past it.
	ws, err := waivers.Parse(data)
	if err != nil {
		slog.Warn("ignoring repo waivers", "repo", slug, "file", file, "err", err)
		return nil
	}
	return ws
}

func (c GitLabAPI) branches(ctx context.Context, id int64, slug string) []string {
	names, err := c.Client.Branches(ctx, id, 100)
	if err != nil {
		c.observe(err)
		slog.Warn("failed to list branches", "repo", slug, "err", err)
	}
	return names
}
