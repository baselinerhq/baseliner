package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	// QuietPrivate leaves skipped non-public repos out of the debug log
	// entirely, for exclude mode in a public context, where even a
	// "(private)" line would show that one exists.
	QuietPrivate bool
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
	// Before any repo is named in a log line or a source: on a hidden
	// instance its public repos are internal everywhere.
	hidden, err := d.publicHidden(ctx, repos)
	if err != nil {
		return nil, err
	}
	if hidden {
		for _, r := range repos {
			if logName(r) != "(private)" {
				r.Visibility = github.Ptr("internal")
			}
		}
	}

	var sources []source.Repo
	archived := 0
	for _, repo := range repos {
		name := repo.GetName()
		if repo.GetArchived() && !d.Cfg.IncludeArchived {
			d.logSkip("skipping archived repo", repo)
			// In exclude mode a count that included a private repo would show
			// that one exists.
			if !d.QuietPrivate || logName(repo) != "(private)" {
				archived++
			}
			continue
		}
		if d.isExcluded(name) {
			d.logSkip("excluding repo (exclude pattern)", repo)
			continue
		}
		if !d.isIncluded(name) {
			d.logSkip("skipping repo (not in include list)", repo)
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

// publicHidden reports whether the API is not github.com's and hides public
// repos from anonymous visitors, as GitHub Enterprise Server in private mode
// does: every user must sign in, while the API still calls repos public that
// only the instance's users can see. It reads the first repo the API calls
// public, archived or filtered out or not, without the token. A refusal
// means hidden; an answer that settles nothing, such as a 5xx, is an error
// after one retry, so a blip never changes what a scan shows. github.com has
// no such mode.
func (d GitHub) publicHidden(ctx context.Context, repos []*github.Repository) (bool, error) {
	if d.Client.BaseURL == nil || isDotCom(d.Client.BaseURL) {
		return false, nil
	}
	for _, r := range repos {
		if logName(r) == "(private)" {
			continue
		}
		owner := r.GetOwner().GetLogin()
		if owner == "" {
			owner = d.Cfg.Name
		}
		visible, err := d.anonymousVisible(ctx, owner, r.GetName())
		if err != nil {
			visible, err = d.anonymousVisible(ctx, owner, r.GetName())
		}
		if err != nil {
			return false, config.NewConfigError("could not tell whether the GitHub instance at %s hides public repos from anonymous visitors: "+
				"reading one public repo without the token failed twice (%v). baseliner checks this so that repos on an instance in "+
				"private mode are not shown as public. Make sure unauthenticated API requests reach the instance (proxy, firewall, "+
				"rate limits for anonymous traffic), then run again", d.Client.BaseURL.Hostname(), err)
		}
		if !visible {
			slog.Info("the GitHub instance hides public repos from anonymous visitors (private mode); they are treated as internal")
		}
		return !visible, nil
	}
	return false, nil
}

// isDotCom reports whether u is github.com's API.
func isDotCom(u *url.URL) bool {
	return strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "api.github.com")
}

// anonymousVisible reports whether owner/name can be read without any
// credential: a 200 whose body is that repo. A refusal (401, 403, 404) or a
// redirect, which is not followed, is not visible; a sign-in page that
// answers 200 is not the repo either. Any other answer is an error.
func (d GitHub) anonymousVisible(ctx context.Context, owner, name string) (bool, error) {
	u := d.Client.BaseURL.JoinPath("repos", owner, name)
	u.User = nil // credentials in GITHUB_API_URL would be sent as Basic auth
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, errors.New("building the request failed")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		// Not the *url.Error itself: it quotes the URL, which names a repo
		// that may turn out to be hidden.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		var got struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&got) != nil {
			return false, nil
		}
		return strings.EqualFold(got.Name, name), nil
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		// GHES with rate limits on refuses an exhausted anonymous quota with
		// 403: that says nothing about visibility.
		return false, errors.New("HTTP 403, anonymous rate limit exhausted")
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound, resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, nil
	}
	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// slug is the repo's owner/name, with the owner spelled as the config spells
// it, so repo_ignores keys keep working. An org scope lists only the org's
// repos (under a renamed org, GitHub reports the new login; the slug keeps
// the configured name). A user scope also lists repos owned by others
// (organisations the user belongs to, collaborations), which take their own
// owner's login.
func (d GitHub) slug(repo *github.Repository) string {
	owner := repo.GetOwner().GetLogin()
	if d.Cfg.Type != "user" || owner == "" || strings.EqualFold(owner, d.Cfg.Name) {
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

// logSkip logs a skipped repo at debug level, unless QuietPrivate is set
// and it is not public.
func (d GitHub) logSkip(msg string, r *github.Repository) {
	if n := logName(r); n != "(private)" || !d.QuietPrivate {
		slog.Debug(msg, "repo", n)
	}
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
