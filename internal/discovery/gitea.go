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

	var sources []source.Repo
	archived := 0
	for i := range repos {
		r := repos[i]
		if r.Archived && !d.Cfg.IncludeArchived {
			d.logSkip("skipping archived repo", r)
			if !d.QuietPrivate || gitea.Visibility(r) == "public" {
				archived++
			}
			continue
		}
		if d.matchesAny(d.Exclude, r.Name) {
			d.logSkip("excluding repo (exclude pattern)", r)
			continue
		}
		if len(d.Include) > 0 && !d.matchesAny(d.Include, r.Name) {
			d.logSkip("skipping repo (not in include list)", r)
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
			Visibility: gitea.Visibility(r),
			ForgeRepo:  &r,
			Aliases:    aliases,
		})
	}
	if archived > 0 {
		slog.Info("skipped archived repos; set scope.gitea.include_archived to scan them", "count", archived)
	}
	d.confirmPublic(ctx, sources)
	return sources, nil
}

// confirmPublic checks one repo the API calls public without a token. When
// it cannot be read, the instance requires sign-in to view anything, so no
// repo there is public: each is treated as internal, which the privacy
// guard protects.
func (d Gitea) confirmPublic(ctx context.Context, sources []source.Repo) {
	for _, s := range sources {
		if s.Visibility != "public" {
			continue
		}
		r := s.ForgeRepo.(*gitea.Repo)
		if d.Client.AnonymousVisible(ctx, r.Owner.Login, r.Name) {
			return
		}
		n := 0
		for i := range sources {
			if sources[i].Visibility == "public" {
				sources[i].Visibility = "internal"
				n++
			}
		}
		slog.Info("the Gitea instance hides public repos from anonymous visitors; they are treated as internal", "count", n)
		return
	}
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

// giteaLogName is how a skipped repo appears in debug logs: by name only
// when it is public.
func giteaLogName(r gitea.Repo) string {
	if gitea.Visibility(r) != "public" {
		return "(private)"
	}
	return r.Name
}

// logSkip logs a skipped repo at debug level, unless QuietPrivate is set and
// it is not public.
func (d Gitea) logSkip(msg string, r gitea.Repo) {
	if n := giteaLogName(r); n != "(private)" || !d.QuietPrivate {
		slog.Debug(msg, "repo", n)
	}
}
