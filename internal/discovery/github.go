package discovery

import (
	"context"
	"log/slog"
	"path"
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

// Discover lists repos (paginated), filters by include/exclude globs on repo name,
// and returns sources carrying the *github.Repository for the collector.
func (d GitHub) Discover(ctx context.Context) ([]source.Repo, error) {
	if err := d.checkRateLimit(ctx); err != nil {
		return nil, err
	}

	repos, err := d.list(ctx)
	if err != nil {
		return nil, err
	}

	var sources []source.Repo
	for _, repo := range repos {
		name := repo.GetName()
		if d.isExcluded(name) {
			slog.Debug("excluding repo (exclude pattern)", "repo", name)
			continue
		}
		if !d.isIncluded(name) {
			slog.Debug("skipping repo (not in include list)", "repo", name)
			continue
		}
		sources = append(sources, source.Repo{
			Type:       "github",
			Slug:       d.Cfg.Name + "/" + name,
			GitHubRepo: repo,
		})
	}
	return sources, nil
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

func (d GitHub) isExcluded(name string) bool {
	for _, p := range d.Exclude {
		if ok, _ := path.Match(p, name); ok {
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
		if ok, _ := path.Match(p, name); ok {
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
