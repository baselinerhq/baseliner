package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/gitea"
	"github.com/baselinerhq/baseliner/internal/source"
)

// maxRepoPages bounds a Gitea listing: 50 repos a page.
const maxRepoPages = 400

// Gitea discovers the repos of an organisation or a user on a Gitea or
// Forgejo instance, such as Codeberg.
type Gitea struct {
	Client  *gitea.Client
	Cfg     config.GiteaScope
	Include []string
	Exclude []string
	// QuietPrivate leaves skipped non-public repos out of the debug log and
	// the archived count, as for GitHub.
	QuietPrivate bool
}

// Discover lists the repos, skips archived ones unless IncludeArchived, and
// filters by include/exclude globs on the repo name. A repo's visibility is
// the effective one (gitea.Visibility), which accounts for its owner's.
//
// It runs before the privacy guard is installed, so nothing it logs or
// returns names a repo that is not public: skipped repos are logged through
// giteaLogName, and an API failure is reported by its status alone.
func (d Gitea) Discover(ctx context.Context) ([]source.Repo, error) {
	repos, complete, err := d.Client.Repos(ctx, d.Cfg.Type, d.Cfg.Name, maxRepoPages)
	if err != nil {
		switch gitea.Status(err) {
		case http.StatusNotFound:
			return nil, config.NewConfigError("Gitea %s %q not found, or not visible to the token", d.Cfg.Type, d.Cfg.Name)
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, config.NewAuthError("Gitea refused the token in '%s' (HTTP %d)", d.Cfg.TokenEnv, gitea.Status(err))
		case http.StatusTooManyRequests:
			return nil, config.NewRateLimitError("Gitea rate limit exceeded while listing repos. Try again later.")
		}
		if s := gitea.Status(err); s != 0 {
			return nil, fmt.Errorf("listing the Gitea %s's repos failed: HTTP %d", d.Cfg.Type, s)
		}
		return nil, fmt.Errorf("listing the Gitea %s's repos failed: %w", d.Cfg.Type, err)
	}
	if !complete {
		return nil, fmt.Errorf("the Gitea %s has more than %d repos; narrow the scope", d.Cfg.Type, maxRepoPages*50)
	}

	// Before anything is logged: on an instance that hides public repos from
	// anonymous visitors, none of them may be named, skipped ones included.
	hidden, err := d.publicHidden(ctx, repos)
	if err != nil {
		return nil, err
	}
	visibility := func(r gitea.Repo) string {
		if v := gitea.Visibility(r); v != "public" || !hidden {
			return v
		}
		return "internal"
	}

	var sources []source.Repo
	archived := 0
	for i := range repos {
		r := repos[i]
		v := visibility(r)
		if r.Archived && !d.Cfg.IncludeArchived {
			d.logSkip("skipping archived repo", r.Name, v)
			if !d.QuietPrivate || v == "public" {
				archived++
			}
			continue
		}
		if d.matchesAny(d.Exclude, r.Name) {
			d.logSkip("excluding repo (exclude pattern)", r.Name, v)
			continue
		}
		if len(d.Include) > 0 && !d.matchesAny(d.Include, r.Name) {
			d.logSkip("skipping repo (not in include list)", r.Name, v)
			continue
		}
		slug := d.slug(r)
		var aliases []string
		if r.FullName != "" && r.FullName != slug {
			aliases = append(aliases, r.FullName)
		}
		sources = append(sources, source.Repo{
			Type:       "gitea",
			Slug:       slug,
			Visibility: v,
			ForgeRepo:  &r,
			Aliases:    aliases,
		})
	}
	if archived > 0 {
		slog.Info("skipped archived repos; set scope.gitea.include_archived to scan them", "count", archived)
	}
	return sources, nil
}

// publicHidden reports whether the instance hides public repos from
// anonymous visitors, as one that requires sign-in to view anything does
// while its API still calls them public. It reads the first repo the API
// calls public, archived or filtered out or not, without the token. A
// refusal means hidden; an answer that settles nothing, such as a 5xx, is an
// error after one retry, so a blip never changes what a scan shows.
func (d Gitea) publicHidden(ctx context.Context, repos []gitea.Repo) (bool, error) {
	for _, r := range repos {
		if gitea.Visibility(r) != "public" {
			continue
		}
		visible, err := d.Client.AnonymousVisible(ctx, r.Owner.Login, r.Name)
		if err != nil {
			visible, err = d.Client.AnonymousVisible(ctx, r.Owner.Login, r.Name)
		}
		if err != nil {
			return false, config.NewConfigError("could not tell whether the Gitea instance hides public repos from anonymous visitors: "+
				"reading one public repo without the token failed twice (%v). baseliner checks this so that repos on an instance that "+
				"requires sign-in are not shown as public. Make sure unauthenticated API requests reach the instance (proxy, firewall, "+
				"rate limits for anonymous traffic), then run again", err)
		}
		if !visible {
			slog.Info("the Gitea instance hides public repos from anonymous visitors; they are treated as internal")
		}
		return !visible, nil
	}
	return false, nil
}

// slug is owner/name, with the owner spelled as the config spells it, as for
// GitHub; a user scope's repos owned by others take their owner's login.
func (d Gitea) slug(r gitea.Repo) string {
	owner := r.Owner.Login
	if d.Cfg.Type != "user" || owner == "" || strings.EqualFold(owner, d.Cfg.Name) {
		owner = d.Cfg.Name
	}
	return owner + "/" + r.Name
}

func (d Gitea) matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if globMatch(p, name) {
			return true
		}
	}
	return false
}

// logSkip logs a skipped repo at debug level, by name only when its
// visibility is public, and not at all when it is not and QuietPrivate is set.
func (d Gitea) logSkip(msg, name, visibility string) {
	switch {
	case visibility == "public":
		slog.Debug(msg, "repo", name)
	case !d.QuietPrivate:
		slog.Debug(msg, "repo", "(private)")
	}
}
