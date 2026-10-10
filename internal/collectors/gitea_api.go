package collectors

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
	"github.com/baselinerhq/baseliner/internal/waivers"
)

// giteaEvidenceDirs are the directories listed for a Gitea or Forgejo repo:
// the same as for GitHub, and where Gitea and Forgejo Actions, Woodpecker CI
// and CODEOWNERS can also live.
var giteaEvidenceDirs = append(slices.Clone(evidenceDirs),
	".gitea", ".gitea/workflows", ".forgejo", ".forgejo/workflows", ".woodpecker")

// GiteaAPI collects a Gitea or Forgejo repo's evidence through the API. It
// reads no platform evidence, so the platform checks report unknown, and no
// CI state, so ci_present uses file presence.
type GiteaAPI struct {
	Client             *gitea.Client
	StaleThresholdDays int
	Now                func() time.Time
	// Observe, when set, is passed each API error, as for GitHub.
	Observe func(error)
}

// NewGiteaAPI returns a collector with the default 90-day stale threshold.
func NewGiteaAPI(c *gitea.Client) GiteaAPI {
	return GiteaAPI{Client: c, StaleThresholdDays: defaultStaleThresholdDays, Now: time.Now}
}

func (c GiteaAPI) observe(err error) {
	if c.Observe != nil {
		c.Observe(err)
	}
}

// Collect reads the repo behind src. Gitea answers a missing directory and a
// missing ref with the same 404, so a directory's 404 counts as absence only
// once the root listing has shown the ref exists; any other failure is
// unread, and the checks that look there report unknown. Every warning
// carries the slug, so the privacy guard can mask or drop it.
func (c GiteaAPI) Collect(ctx context.Context, src source.Repo) *models.NormalizedRepository {
	r, _ := src.ForgeRepo.(*gitea.Repo)
	if r == nil {
		return emptyResult(src)
	}
	owner, name, ref := r.Owner.Login, r.Name, r.DefaultBranch

	var files, unread []string
	var readme *string
	readmeOK := true
	var branches []string
	var lastCommit *time.Time
	switch {
	case r.Empty:
		// Nothing to read: every file is genuinely absent.
	case ref == "":
		// No default branch on a repo that is not empty: it could not be
		// read, which is not absence.
		unread = slices.Clone(giteaEvidenceDirs)
		readmeOK = false
		slog.Warn("gitea repository not readable: no default branch reported", "repo", src.Slug)
	default:
		rootRead := false
		for _, dir := range giteaEvidenceDirs {
			got, ok := c.listFiles(ctx, owner, name, ref, dir, rootRead, src.Slug)
			files = append(files, got...)
			if !ok {
				unread = append(unread, dir)
			} else if dir == "" {
				rootRead = true
			}
		}
		files = dedupeSort(files)
		readme, readmeOK = c.readme(ctx, owner, name, ref, files, src.Slug)
		if !rootRead {
			// The README lives at the root: with no root listing, no README
			// listed is no evidence there is none.
			readmeOK = false
		}
		branches = c.branches(ctx, owner, name, src.Slug)
		if t, err := c.Client.LastCommit(ctx, owner, name, ref); err == nil && !t.IsZero() {
			t = t.UTC()
			lastCommit = &t
		} else if err != nil {
			c.observe(err)
			slog.Warn("could not read the last commit", "repo", src.Slug, "err", err)
		}
	}
	if files == nil {
		files = []string{}
	}
	if branches == nil {
		branches = []string{}
	}

	var days *int
	isStale := false
	if lastCommit != nil {
		d := int(c.Now().Sub(*lastCommit).Hours() / 24)
		days = &d
		isStale = d > c.StaleThresholdDays
	}
	var defaultBranch *string
	if ref != "" {
		defaultBranch = &ref
	}

	return &models.NormalizedRepository{
		SourceType: models.SourceGitea,
		Slug:       src.Slug,
		Name:       name,
		Visibility: src.Visibility,
		Waivers:    c.waivers(ctx, owner, name, ref, files, src.Slug),
		FS: &models.FilesystemContext{
			Files:          files,
			KeyFiles:       DetectKeyFiles(files),
			ReadmeContent:  readme,
			CIFiles:        DetectCIFiles(files),
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

// listFiles returns the files directly under dir, none when it does not
// exist, and false when it could not be read. A 404 means absence only for a
// directory below a root that was read: before that, it could be a missing
// ref. A path that is a file is no directory. Only files count: a symlink or a
// submodule is not one, as for GitHub.
func (c GiteaAPI) listFiles(ctx context.Context, owner, name, ref, dir string, rootRead bool, slug string) ([]string, bool) {
	entries, isDir, err := c.Client.Contents(ctx, owner, name, ref, dir)
	if err != nil {
		if dir != "" && rootRead && gitea.Status(err) == http.StatusNotFound {
			return nil, true
		}
		c.observe(err)
		slog.Warn("gitea contents listing failed", "repo", slug, "dir", dir, "err", err)
		return nil, false
	}
	if !isDir {
		return nil, true
	}
	var out []string
	for _, e := range entries {
		if e.Type == "file" {
			out = append(out, e.Path)
		}
	}
	return out, true
}

// readme returns the listed README's first maxReadmeBytes as UTF-8, nil when
// none is listed, and false when it is listed but could not be read (a 404
// included: the file was just listed).
func (c GiteaAPI) readme(ctx context.Context, owner, name, ref string, files []string, slug string) (*string, bool) {
	rel := FindReadmePath(files)
	if rel == "" {
		return nil, true
	}
	data, err := c.Client.RawFile(ctx, owner, name, ref, rel, maxReadmeBytes)
	if err != nil {
		c.observe(err)
		slog.Warn("could not read README", "repo", slug, "err", err)
		return nil, false
	}
	s := strings.ToValidUTF8(string(data), "�")
	return &s, true
}

// waivers reads the waiver file when the root listing has one. A file that
// cannot be read or does not parse declares no waivers: the checks run, and a
// warning says why.
func (c GiteaAPI) waivers(ctx context.Context, owner, name, ref string, files []string, slug string) []models.Waiver {
	file := waiverFile(files, slug)
	if file == "" {
		return nil
	}
	data, err := c.Client.RawFile(ctx, owner, name, ref, file, waivers.MaxBytes+1)
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

func (c GiteaAPI) branches(ctx context.Context, owner, name, slug string) []string {
	names, err := c.Client.Branches(ctx, owner, name, 100)
	if err != nil {
		c.observe(err)
		slog.Warn("failed to list branches", "repo", slug, "err", err)
	}
	return names
}
