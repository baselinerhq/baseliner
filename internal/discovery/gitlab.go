package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/source"
)

// maxProjectPages bounds the group listing: 100 projects a page.
const maxProjectPages = 500

// GitLab discovers the projects of a GitLab group and its subgroups.
type GitLab struct {
	Client  *gitlab.Client
	Cfg     config.GitLabScope
	Include []string
	Exclude []string
}

// Discover looks the group up, lists its projects, skips archived ones unless
// IncludeArchived, and filters by include/exclude globs on the project's path
// relative to the group ("team/app"). Each source's slug is the project's
// full path as GitLab spells it; the config's spelling and the URL-encoded
// forms are aliases, for the privacy guard.
//
// It runs before the privacy guard is installed, so nothing it logs or
// returns names a project that is not public: skipped projects are logged
// through glLogName, and an API failure is reported by its status alone.
func (d GitLab) Discover(ctx context.Context) ([]source.Repo, error) {
	group, err := d.Client.Group(ctx, d.Cfg.Group)
	if err != nil {
		switch gitlab.Status(err) {
		case http.StatusNotFound:
			return nil, config.NewConfigError("GitLab group %q not found, or not visible to the token", d.Cfg.Group)
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, config.NewAuthError("GitLab refused the token in '%s' (HTTP %d); it needs the read_api scope", d.Cfg.TokenEnv, gitlab.Status(err))
		}
		return nil, statusError("looking up the GitLab group", err)
	}
	projects, complete, err := d.Client.GroupProjects(ctx, group.ID, maxProjectPages)
	if err != nil {
		return nil, statusError("listing the GitLab group's projects", err)
	}
	if !complete {
		return nil, fmt.Errorf("the GitLab group has more than %d projects; narrow scope.gitlab.group", maxProjectPages*100)
	}

	prefix := strings.ToLower(group.FullPath) + "/"
	var sources []source.Repo
	archived := 0
	for i := range projects {
		p := projects[i]
		full := p.PathWithNamespace
		if !strings.HasPrefix(strings.ToLower(full), prefix) {
			slog.Debug("skipping project outside the group", "project", glLogName(p))
			continue
		}
		rel := full[len(prefix):]
		if p.Archived && !d.Cfg.IncludeArchived {
			slog.Debug("skipping archived project", "project", glLogName(p))
			archived++
			continue
		}
		if d.matchesAny(d.Exclude, rel) {
			slog.Debug("excluding project (exclude pattern)", "project", glLogName(p))
			continue
		}
		if len(d.Include) > 0 && !d.matchesAny(d.Include, rel) {
			slog.Debug("skipping project (not in include list)", "project", glLogName(p))
			continue
		}
		aliases := []string{url.PathEscape(full)}
		if cfgSpelling := d.Cfg.Group + "/" + rel; cfgSpelling != full {
			aliases = append(aliases, cfgSpelling, url.PathEscape(cfgSpelling))
		}
		sources = append(sources, source.Repo{
			Type:       "gitlab",
			Slug:       full,
			Visibility: gitlab.Visibility(p),
			ForgeRepo:  &p,
			Aliases:    aliases,
		})
	}
	// As for GitHub: if every match was archived the scan finds nothing, and
	// this is the only line that says why. A count names no project.
	if archived > 0 {
		slog.Info("skipped archived projects; set scope.gitlab.include_archived to scan them", "count", archived)
	}
	return sources, nil
}

func (d GitLab) matchesAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		if globMatch(p, rel) {
			return true
		}
	}
	return false
}

// glLogName is how a skipped project appears in debug logs: by its path only
// when it is public, as logName does for GitHub.
func glLogName(p gitlab.Project) string {
	if gitlab.Visibility(p) != "public" {
		return "(private)"
	}
	return p.PathWithNamespace
}

// statusError reports a failed discovery call by its HTTP status only: the
// server's message is not shown, since discovery output is not redacted. A
// rate-limit refusal says when the limit lifts.
func statusError(what string, err error) error {
	var rl *gitlab.RateLimitError
	if errors.As(err, &rl) {
		when := "later"
		if !rl.Reset.IsZero() {
			when = "at " + rl.Reset.UTC().Format(time.RFC3339)
		}
		return config.NewRateLimitError("GitLab rate limit exceeded while %s. Resets %s. Try again then.", what, when)
	}
	if s := gitlab.Status(err); s != 0 {
		return fmt.Errorf("%s failed: HTTP %d", what, s)
	}
	return fmt.Errorf("%s failed: %w", what, err)
}
