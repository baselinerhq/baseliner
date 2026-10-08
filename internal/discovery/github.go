package discovery

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/source"
)

// GitHub discovers repositories from a GitHub org or user.
type GitHub struct {
	Client  *github.Client
	Cfg     config.GitHubScope
	Include []string
	Exclude []string
}

// Discover lists repos (paginated), skips archived repos unless
// IncludeArchived, filters by include/exclude globs on repo name, and returns
// sources carrying the *github.Repository for the collector.
func (d GitHub) Discover(ctx context.Context) ([]source.Repo, error) {
	if err := d.checkRateLimit(ctx); err != nil {
		return nil, err
	}

	repos, err := d.list(ctx)
	if err != nil {
		return nil, err
	}

	var sources []source.Repo
	archived := 0
	for _, repo := range repos {
		name := repo.GetName()
		if repo.GetArchived() && !d.Cfg.IncludeArchived {
			slog.Debug("skipping archived repo", "repo", logName(repo))
			archived++
			continue
		}
		if d.isExcluded(name) {
			slog.Debug("excluding repo (exclude pattern)", "repo", logName(repo))
			continue
		}
		if !d.isIncluded(name) {
			slog.Debug("skipping repo (not in include list)", "repo", logName(repo))
			continue
		}
		sources = append(sources, source.Repo{
			Type:       "github",
			Slug:       d.slug(repo),
			GitHubRepo: repo,
		})
	}
	// Say so at info level: if every match was archived the scan finds nothing
	// and exits 2, and this is the only line that says why. A count names no repo.
	if archived > 0 {
		slog.Info("skipped archived repos; set scope.github.include_archived to scan them", "count", archived)
	}
	return sources, nil
}

// slug is the repo's owner/name. The owner is spelled as the config spells it
// when it is the configured org or user, so repo_ignores keys keep working; a
// user scope also lists repos owned by others (organisations the user belongs
// to, collaborations), which take their own owner's login.
func (d GitHub) slug(repo *github.Repository) string {
	owner := repo.GetOwner().GetLogin()
	if owner == "" || strings.EqualFold(owner, d.Cfg.Name) {
		owner = d.Cfg.Name
	}
	return owner + "/" + repo.GetName()
}

func (d GitHub) list(ctx context.Context) ([]*github.Repository, error) {
	var all []*github.Repository
	if d.Cfg.Type == "org" {
		opt := &github.RepositoryListByOrgOptions{ListOptions: github.ListOptions{PerPage: 100}}
		for {
			page, resp, err := d.Client.Repositories.ListByOrg(ctx, d.Cfg.Name, opt)
			if err != nil {
				return nil, err
			}
			all = append(all, page...)
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
		return all, nil
	}
	opt := &github.RepositoryListByUserOptions{Type: "all", ListOptions: github.ListOptions{PerPage: 100}}
	for {
		page, resp, err := d.Client.Repositories.ListByUser(ctx, d.Cfg.Name, opt)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return all, nil
}

// logName is how a filtered-out repo appears in debug logs. A private or
// internal repo is never named: it is never scanned, so the privacy guard's
// redaction (keyed on scanned repos) cannot know about it, and excluding a
// private repo is often how it is kept out of a public report.
func logName(r *github.Repository) string {
	if v := r.GetVisibility(); r.GetPrivate() || (v != "" && !strings.EqualFold(v, "public")) {
		return "(private)"
	}
	return r.GetName()
}

func (d GitHub) isExcluded(name string) bool {
	for _, p := range d.Exclude {
		if globMatch(p, name) {
			return true
		}
	}
	return false
}

func (d GitHub) isIncluded(name string) bool {
	if len(d.Include) == 0 {
		return true
	}
	for _, p := range d.Include {
		if globMatch(p, name) {
			return true
		}
	}
	return false
}

func (d GitHub) checkRateLimit(ctx context.Context) error {
	rl, _, err := d.Client.RateLimit.Get(ctx)
	if err != nil {
		slog.Debug("could not check rate limit", "err", err)
		return nil
	}
	core := rl.GetCore()
	if core == nil {
		return nil
	}
	if core.Remaining == 0 {
		return config.NewRateLimitError(
			"Rate limit exceeded. Resets at %s. Try again later.",
			core.Reset.Format(time.RFC3339))
	}
	if core.Remaining < 100 {
		slog.Warn("GitHub API rate limit low", "remaining", core.Remaining,
			"reset", core.Reset.Format(time.RFC3339))
	}
	return nil
}
