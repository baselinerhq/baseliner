// Package runner orchestrates a scan: config -> discovery -> collect -> evaluate
// -> output -> (issues). It returns a process exit code so the CLI stays thin and
// the pipeline is unit-testable.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"golang.org/x/sync/errgroup"

	"github.com/baselinerhq/baseliner/internal/actions"
	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/collectors"
	"github.com/baselinerhq/baseliner/internal/config"
	"github.com/baselinerhq/baseliner/internal/discovery"
	"github.com/baselinerhq/baseliner/internal/engine"
	"github.com/baselinerhq/baseliner/internal/gitlab"
	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/output"
	"github.com/baselinerhq/baseliner/internal/policy"
	"github.com/baselinerhq/baseliner/internal/privacy"
	"github.com/baselinerhq/baseliner/internal/source"
)

// Options are the resolved scan flags.
type Options struct {
	ConfigPath   string
	OutputFile   string
	SarifFile    string // when set, also write SARIF 2.1.0 here (for code scanning)
	MarkdownFile string // when set, also write a Markdown report here (for issues/PR comments)
	Format       string // "json" | "table" | "both"
	OpenIssues   bool
	DryRun       bool
	Quiet        bool
	// FailUnder, when set, replaces the default per-check gate: the scan exits 1
	// if any repo scores below the threshold (every repo must be >= it), else 0.
	FailUnder *float64
	// MinCoverage, when set, additionally requires each repo's evidence coverage
	// to reach this fraction. It composes with FailUnder and with the default
	// per-check gate rather than replacing either: posture grades what was
	// observed, coverage grades how much could be observed.
	MinCoverage *float64
	// PublicContext, when non-nil, overrides config's privacy.public_context:
	// it signals the output is public, activating the privacy guard. nil means
	// "use the config value" (mirrors the --public-context flag being unset).
	PublicContext *bool
	// GitHubActions reports that the scan runs under GitHub Actions
	// (GITHUB_ACTIONS=true). Its log and artifacts are public whenever the
	// repo is, so there an unset public context counts as public.
	GitHubActions bool
}

// builtinHandler is slog's built-in default handler, captured before anything
// can replace it. It writes through the log package, which slog.SetDefault
// redirects back into slog, so the privacy guard must never wrap it.
var builtinHandler = slog.Default().Handler()

// Scan runs the pipeline and returns the process exit code (0 pass, 1 failures, 2 error).
func Scan(stdout, stderr io.Writer, opts Options) (code int) {
	switch opts.Format {
	case "json", "table", "both":
	default:
		fmt.Fprintf(stderr, "invalid --format %q: must be json, table, or both\n", opts.Format)
		return 2
	}
	if opts.MinCoverage != nil && (*opts.MinCoverage < 0 || *opts.MinCoverage > 1) {
		fmt.Fprintf(stderr, "invalid --min-coverage %.4g: must be between 0.0 and 1.0\n", *opts.MinCoverage)
		return 2
	}
	if opts.FailUnder != nil && (*opts.FailUnder < 0 || *opts.FailUnder > 1) {
		fmt.Fprintf(stderr, "invalid --fail-under %.4g: must be between 0.0 and 1.0\n", *opts.FailUnder)
		return 2
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return mapError(stderr, err)
	}
	pol, err := policy.Load(cfg.Policy.Base)
	if err != nil {
		return mapError(stderr, err)
	}
	registry := checks.BuildDefault()
	if err := cfg.ValidateCheckIDs(func(id string) bool { _, ok := registry.Get(id); return ok }); err != nil {
		return mapError(stderr, err)
	}
	eng := engine.New(pol, registry, cfg.Policy.Ignore, cfg.Policy.RepoIgnores)
	for _, rule := range cfg.Policy.IgnoreWhen {
		eng.IgnoreWhen = append(eng.IgnoreWhen, engine.VisibilityIgnore{Visibility: rule.Visibility, Checks: rule.Checks})
	}
	if rw := cfg.Policy.RepoWaivers; rw != nil {
		eng.WaivableChecks = map[string]bool{}
		for _, id := range rw.Allow {
			eng.WaivableChecks[id] = true
		}
	}
	platform := needsPlatform(pol, registry, cfg.Policy.Ignore)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	po := privacyOptions(cfg, opts)
	sources, clients, err := discover(ctx, cfg, po.PublicContext && po.Mode == privacy.ModeExclude)
	if err != nil {
		return mapError(stderr, err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(stderr, "No repositories discovered. Check your scope config.")
		return 2
	}

	// From here on, log lines and stderr messages can name scanned repos; in a
	// public context redact the private ones there too, not only in the
	// results view below.
	stderr, restore := guardStderr(stderr, sources, cfg, opts)
	defer restore()
	// From here a panic would reach the runtime, which prints it straight to
	// the process's stderr, past the guard. Report it through the guarded
	// stderr instead, as a run that broke.
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "internal error: %v\n%s", p, debug.Stack())
			code = 2
		}
	}()
	// A scan GitHub rate-limited is incomplete, however it ends.
	limits := &rateLimitWatch{}
	defer func() {
		if msg := limits.summary(); msg != "" {
			fmt.Fprintln(stderr, msg)
			code = max(code, 2)
		}
	}()
	if publicContextInferred(cfg, opts) && privacyOptions(cfg, opts).Mode != privacy.ModeAllow {
		fmt.Fprintln(stderr, "privacy guard on: running under GitHub Actions with no public context set. "+
			"If this run's log and artifacts are private, set privacy.public_context: false or pass --public-context=false.")
	}

	now := time.Now().UTC()
	repos, collErrors := collectAll(ctx, sources, clients.collectors(platform, limits.observe), now)
	run := eng.RunBatch(repos, now)
	if len(collErrors) > 0 {
		run = mergeCollectionErrors(run, collErrors)
	}

	// Build the disclosure-facing view: in a public context, private/internal
	// repos are redacted/excluded so the aggregate output (console logs, JSON
	// and SARIF artifacts) never leaks them. The authoritative `run` is left
	// untouched for issue-opening (private issues stay private) and the
	// exit-code gate (which must still fail on a private repo's findings).
	view, err := privacy.Apply(run, repoVisibility(sources), privacyOptions(cfg, opts))
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	if formatHasJSON(opts.Format) {
		if err := output.WriteJSON(stdout, &view, opts.OutputFile); err != nil {
			fmt.Fprintf(stderr, "Error: could not write JSON output: %v\n", err)
			return 2
		}
	}
	if formatHasTable(opts.Format) && !opts.Quiet {
		output.PrintSummary(stdout, &view)
	}

	if opts.SarifFile != "" {
		if err := output.WriteSARIF(&view, opts.SarifFile); err != nil {
			fmt.Fprintf(stderr, "Error: could not write SARIF output: %v\n", err)
			return 2
		}
	}

	if opts.MarkdownFile != "" {
		if err := output.WriteMarkdown(&view, opts.MarkdownFile); err != nil {
			fmt.Fprintf(stderr, "Error: could not write Markdown output: %v\n", err)
			return 2
		}
	}

	// An issue-delivery failure (exit 2) outranks a gate failure (exit 1), but
	// the gates still run so their lists are printed.
	excluded := privacy.Excluded(repoVisibility(sources), privacyOptions(cfg, opts))
	issueCode := 0
	if opts.OpenIssues {
		issueCode = openIssues(ctx, stderr, cfg, clients.github, sources, run, opts.DryRun, excluded != nil, limits)
	}
	return max(issueCode, gate(stderr, opts, run, excluded))
}

// repoList joins the entries of a gate list. Repos hidden by exclude mode are
// counted at the end rather than named, with no score.
func repoList(shown []string, hidden int) string {
	if hidden > 0 {
		shown = append(shown, fmt.Sprintf("%d private repo(s)", hidden))
	}
	return strings.Join(shown, ", ")
}

// gate applies --min-coverage, then --fail-under or the default per-check gate,
// printing the repos that fail it, and returns the exit code (0 or 1). When
// excluded is non-nil, the repos it reports are counted, not named.
func gate(stderr io.Writer, opts Options, run models.RunResult, excluded func(string) bool) int {
	hide := func(slug string) bool { return excluded != nil && excluded(slug) }
	// Coverage is gated independently of posture: a repo whose evidence could not
	// be read must not pass on the strength of the few checks that did resolve.
	if opts.MinCoverage != nil {
		var under []string
		hidden := 0
		for _, rr := range run.Repos {
			if float64(rr.Coverage) >= *opts.MinCoverage {
				continue
			}
			if hide(rr.Slug) {
				hidden++
				continue
			}
			under = append(under, fmt.Sprintf("%s (%.0f%%)", rr.Slug, float64(rr.Coverage)*100))
		}
		if n := len(under) + hidden; n > 0 {
			fmt.Fprintf(stderr, "%d repo(s) below --min-coverage %.0f%%: %s\n",
				n, *opts.MinCoverage*100, repoList(under, hidden))
			return 1
		}
	}

	if opts.FailUnder != nil {
		var below []string
		hidden := 0
		for _, rr := range run.Repos {
			posture, ok := rr.Posture()
			if (!ok || posture < *opts.FailUnder) && hide(rr.Slug) {
				hidden++
				continue
			}
			if !ok {
				// Nothing conclusive was observed, so compliance cannot be
				// demonstrated. Fail closed rather than treating the absence of
				// evidence as a passing score.
				below = append(below, fmt.Sprintf("%s (not assessed)", rr.Slug))
				continue
			}
			if posture < *opts.FailUnder {
				below = append(below, fmt.Sprintf("%s (%.2f)", rr.Slug, posture))
			}
		}
		if n := len(below) + hidden; n > 0 {
			fmt.Fprintf(stderr, "%d repo(s) below --fail-under %.2f: %s\n",
				n, *opts.FailUnder, repoList(below, hidden))
			return 1
		}
		return 0
	}

	if run.Failed > 0 {
		// The table explains a red run, but exclude mode leaves private repos
		// out of it, so count the ones that failed.
		hidden := 0
		for _, rr := range run.Repos {
			if hide(rr.Slug) && hasFailOrError(rr) {
				hidden++
			}
		}
		if hidden > 0 {
			fmt.Fprintf(stderr, "%d private repo(s) failed the baseline; privacy.private_repos: exclude leaves them out of the output\n", hidden)
		}
		return 1
	}
	return 0
}

// openIssues opens/updates findings issues for GitHub repos. A per-repo failure
// (a failed search or write) is logged and delivery continues for the rest; if
// any failed it returns exit 2 at the end, because a run that delivered nothing must not read as
// green (in monitor mode the findings themselves never fail the run).
// When excluding is set, the log omits warnings about private repos (exclude
// mode drops them), and the summary says so.
func openIssues(ctx context.Context, stderr io.Writer, cfg *config.Config, client *github.Client, sources []source.Repo, run models.RunResult, dryRun, excluding bool, limits *rateLimitWatch) int {
	if cfg.Scope.GitHub == nil && cfg.Scope.GitLab != nil {
		fmt.Fprintln(stderr, "--open-issues delivers findings issues to GitHub repos only; it does not yet support GitLab (#142)")
		return 2
	}
	tokenEnv := "GITHUB_TOKEN"
	if cfg.Scope.GitHub != nil {
		tokenEnv = cfg.Scope.GitHub.TokenEnv
	}
	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		fmt.Fprintf(stderr, "--open-issues requires a GitHub token in '%s'\n", tokenEnv)
		return 2
	}
	if client == nil {
		c, err := newGitHubClient(token)
		if err != nil {
			return mapError(stderr, err)
		}
		client = c
	}

	action := actions.GitHubIssues{Client: client, DryRun: dryRun}
	bySlug := make(map[string]source.Repo, len(sources))
	for _, s := range sources {
		if prev, ok := bySlug[s.Slug]; ok && prev.Type != s.Type && (prev.Type == "github" || s.Type == "github") {
			// Results carry only the slug, so the GitHub repo's findings could
			// not be told from the other forge's (#157).
			fmt.Fprintln(stderr, "--open-issues cannot tell a GitHub repo from another forge's repo with the same path; "+
				"exclude one of them from the scope (#157)")
			return 2
		}
		bySlug[s.Slug] = s
	}
	failed, gitlabSkipped := 0, 0
	for _, rr := range run.Repos {
		s, ok := bySlug[rr.Slug]
		if ok && s.Type == "gitlab" {
			gitlabSkipped++
			continue
		}
		repo, isGH := s.GitHubRepo.(*github.Repository)
		if !ok || !isGH || repo == nil {
			slog.Warn("cannot open issue: no GitHub repo reference", "slug", rr.Slug)
			continue
		}
		// Archived repos and repos with Issues turned off cannot take a findings
		// issue by design; that is not a delivery failure.
		if repo.GetArchived() || (repo.HasIssues != nil && !*repo.HasIssues) {
			slog.Info("findings issue not delivered: repo is archived or has Issues disabled", "slug", rr.Slug)
			continue
		}
		if err := action.Run(ctx, rr, repo.GetOwner().GetLogin(), repo.GetName()); err != nil {
			limits.observe(err)
			slog.Warn("failed to open/update issue", "slug", rr.Slug, "err", err)
			failed++
		}
	}
	if gitlabSkipped > 0 {
		slog.Info("findings issues not delivered to GitLab projects; --open-issues supports GitHub only (#142)", "count", gitlabSkipped)
	}
	if failed > 0 {
		// A count, not slugs: the per-repo warnings above already name them,
		// through the privacy guard.
		note := "see the warnings above"
		if excluding {
			note += "; with privacy.private_repos: exclude, warnings about private repos are not logged"
		}
		fmt.Fprintf(stderr, "could not deliver the findings issue for %d repo(s) (search or write failed); %s\n", failed, note)
		return 2
	}
	return 0
}

// privacyOptions resolves the effective privacy guard settings: the
// --public-context flag (opts) overrides config's privacy.public_context, and
// when neither is set the context is public under GitHub Actions and private
// elsewhere. The mode comes from config (default redact). The mode was already
// validated by config.Load, so ParseMode cannot fail here.
func privacyOptions(cfg *config.Config, opts Options) privacy.Options {
	public := publicContextInferred(cfg, opts)
	modeStr := ""
	if cfg.Privacy != nil {
		if cfg.Privacy.PublicContext != nil {
			public = *cfg.Privacy.PublicContext
		}
		modeStr = cfg.Privacy.PrivateRepos
	}
	if opts.PublicContext != nil {
		public = *opts.PublicContext
	}
	mode, _ := privacy.ParseMode(modeStr)
	return privacy.Options{PublicContext: public, Mode: mode}
}

// publicContextInferred reports that the context is public only because the
// scan runs under GitHub Actions and neither the flag nor the config set it.
func publicContextInferred(cfg *config.Config, opts Options) bool {
	set := opts.PublicContext != nil || (cfg.Privacy != nil && cfg.Privacy.PublicContext != nil)
	return opts.GitHubActions && !set
}

// guardStderr extends the privacy guard to stderr: when it is active, both the
// returned writer and the default slog logger redact private/internal slugs.
// privacy.Apply covers the results view; this covers everything else that
// reaches the log — issue-opening and collection log lines, and the runner's
// own messages such as the --fail-under list, which are built from the
// unredacted run. The returned func restores the previous default logger.
func guardStderr(stderr io.Writer, sources []source.Repo, cfg *config.Config, opts Options) (io.Writer, func()) {
	red := privacy.NewRedactor(withAliases(repoVisibility(sources), sources), privacyOptions(cfg, opts))
	if red == nil {
		return stderr, func() {}
	}
	prev := slog.Default()
	inner := prev.Handler()
	// By type, as slog.SetDefault does: the built-in handler's With and
	// WithGroup return new values of the same type, with the same problem.
	builtin := reflect.TypeOf(inner) == reflect.TypeOf(builtinHandler)
	if builtin {
		// Wrapping the built-in handler would deadlock (see builtinHandler),
		// so log to stderr directly, as the built-in handler would.
		inner = slog.NewTextHandler(stderr, nil)
	}
	// SetDefault points the log package at slog, and setting the built-in
	// handler back does not undo that, so restore log's own output too.
	logOut, logFlags := log.Writer(), log.Flags() //nolint:forbidigo // saved to restore, not to write
	slog.SetDefault(slog.New(red.Handler(inner)))
	return red.Writer(stderr), func() {
		slog.SetDefault(prev)
		if builtin {
			log.SetOutput(logOut) //nolint:forbidigo // restoring the log package's own output
			log.SetFlags(logFlags)
		}
	}
}

// repoVisibility maps each forge source's slug to its visibility
// ("public"|"private"|"internal"), the input the privacy guard uses to decide
// what to protect. Built from sources (not results) so it also covers repos
// that failed collection. Local sources are omitted (treated as public). For
// GitHub it is collectors.Visibility, the value the engine's ignore_when rules
// also see; other forges set it at discovery, and a source without one counts
// as private.
func repoVisibility(sources []source.Repo) map[string]string {
	vis := make(map[string]string, len(sources))
	for _, s := range sources {
		if r, ok := s.GitHubRepo.(*github.Repository); ok && r != nil {
			setVisibility(vis, s.Slug, collectors.Visibility(r))
			continue
		}
		if s.Type == "local" || s.Type == "github" {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(s.Visibility))
		if v == "" {
			v = "private"
		}
		setVisibility(vis, s.Slug, v)
	}
	return vis
}

// setVisibility records v for name unless name is already recorded as
// protected: two sources can share a spelling (the same slug on two forges,
// or one source's alias spelled like another's slug), and the more protective
// visibility must win, or the guard would stop masking the protected one.
func setVisibility(vis map[string]string, name, v string) {
	if old, ok := vis[name]; ok && !strings.EqualFold(old, "public") {
		return
	}
	vis[name] = v
}

// withAliases adds each source's other spellings to vis, with that source's
// visibility: its Aliases, and for GitHub its name as GitHub spells it. A slug
// spells the owner as the config does (but for a user scope's repos owned by
// others), while API URLs, and so the errors that quote them, use the owner's
// login. The redactor already ignores case; this
// covers a login that differs by more than case.
func withAliases(vis map[string]string, sources []source.Repo) map[string]string {
	for _, s := range sources {
		v, known := vis[s.Slug]
		if !known {
			continue
		}
		for _, a := range s.Aliases {
			setVisibility(vis, a, v)
		}
		r, ok := s.GitHubRepo.(*github.Repository)
		if !ok || r == nil {
			continue
		}
		if n := r.GetFullName(); n != "" {
			setVisibility(vis, n, v)
		}
		if login, name := r.GetOwner().GetLogin(), r.GetName(); login != "" && name != "" {
			setVisibility(vis, login+"/"+name, v)
		}
	}
	return vis
}

// newGitHubClient returns an API client for token. GITHUB_API_URL, when set,
// is the API root to use instead of api.github.com — GitHub Actions sets it on
// every runner, to the Enterprise Server API on GHES. A refusal for a missing
// token permission names the permission (permissionHints).
func newGitHubClient(token string) (*github.Client, error) {
	client := github.NewClient(&http.Client{Transport: permissionHints{base: http.DefaultTransport}}).WithAuthToken(token)
	raw := strings.TrimSpace(os.Getenv("GITHUB_API_URL"))
	if raw == "" {
		return client, nil
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, config.NewConfigError("GITHUB_API_URL %q is not an absolute URL", raw)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/" // go-github resolves request paths against a trailing-slash base
	}
	client.BaseURL = u
	return client, nil
}

// forgeClients holds the API client of each forge the scope uses; nil for a
// forge it does not.
type forgeClients struct {
	github *github.Client
	gitlab *gitlab.Client
}

// repoCollector reads one forge source into the normalized model.
type repoCollector interface {
	Collect(ctx context.Context, src source.Repo) *models.NormalizedRepository
}

// collectors returns the collector for each forge with a client, by source
// type.
func (f forgeClients) collectors(platform bool, observe func(error)) map[string]repoCollector {
	cols := map[string]repoCollector{}
	if f.github != nil {
		c := collectors.NewGitHubAPI(f.github)
		c.Platform = platform
		c.Observe = observe
		cols["github"] = c
	}
	if f.gitlab != nil {
		c := collectors.NewGitLabAPI(f.gitlab)
		c.Observe = observe
		cols["gitlab"] = c
	}
	return cols
}

// discover lists every scope's sources. quietPrivate is exclude mode in a
// public context: discovery then logs nothing about a skipped private repo.
func discover(ctx context.Context, cfg *config.Config, quietPrivate bool) ([]source.Repo, forgeClients, error) {
	var sources []source.Repo
	var client *github.Client
	if cfg.Scope.GitHub != nil {
		token := strings.TrimSpace(os.Getenv(cfg.Scope.GitHub.TokenEnv))
		if token == "" {
			return nil, forgeClients{}, config.NewAuthError(
				"GitHub token not found in environment variable '%s'. "+
					"Set it in your environment and re-run the scan.", cfg.Scope.GitHub.TokenEnv)
		}
		c, err := newGitHubClient(token)
		if err != nil {
			return nil, forgeClients{}, err
		}
		client = c
		gh := discovery.GitHub{
			Client:       client,
			Cfg:          *cfg.Scope.GitHub,
			Include:      cfg.Scope.Include,
			Exclude:      cfg.Scope.Exclude,
			QuietPrivate: quietPrivate,
		}
		ghSources, err := gh.Discover(ctx)
		if err != nil {
			return nil, forgeClients{}, err
		}
		sources = append(sources, ghSources...)
	}
	var glClient *gitlab.Client
	if gl := cfg.Scope.GitLab; gl != nil {
		token := strings.TrimSpace(os.Getenv(gl.TokenEnv))
		if token == "" {
			return nil, forgeClients{}, config.NewAuthError(
				"GitLab token not found in environment variable '%s'. "+
					"Set it in your environment and re-run the scan.", gl.TokenEnv)
		}
		c, err := gitlab.New(gl.BaseURL, token)
		if err != nil {
			return nil, forgeClients{}, config.NewConfigError("scope.gitlab.base_url: %v", err)
		}
		glClient = c
		glSources, err := discovery.GitLab{
			Client:       glClient,
			Cfg:          *gl,
			Include:      cfg.Scope.Include,
			Exclude:      cfg.Scope.Exclude,
			QuietPrivate: quietPrivate,
		}.Discover(ctx)
		if err != nil {
			return nil, forgeClients{}, err
		}
		sources = append(sources, glSources...)
	}
	if cfg.Scope.Local != nil && len(cfg.Scope.Local.Paths) > 0 {
		sources = append(sources, discovery.Local{Paths: cfg.Scope.Local.Paths}.Discover()...)
	}
	return sources, forgeClients{github: client, gitlab: glClient}, nil
}

// collectConcurrency bounds parallel collection (I/O-bound: GitHub API + git).
const collectConcurrency = 8

// collectAll collects every source concurrently (bounded) while preserving source
// order in the output — so the console/JSON ordering is identical to a serial run.
func collectAll(ctx context.Context, sources []source.Repo, cols map[string]repoCollector, now time.Time) ([]*models.NormalizedRepository, []models.RepoResult) {
	fsc := collectors.Filesystem{}
	gitc := collectors.NewGit()

	repos := make([]*models.NormalizedRepository, len(sources))
	collErrs := make([]*models.RepoResult, len(sources))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(collectConcurrency)
	for i, src := range sources {
		i, src := i, src
		g.Go(func() (err error) {
			// A panic in a collector becomes a collection_error result for that
			// repo, mirroring the Python CLI's per-source try/except — one bad
			// repo never aborts the fleet scan.
			defer func() {
				if p := recover(); p != nil {
					slog.Warn("failed to collect repo", "slug", src.Slug, "panic", p)
					er := models.NewErrorResult(src.Slug, now, "collection_error", fmt.Sprintf("%v", p))
					collErrs[i] = &er
				}
			}()
			if c, ok := cols[src.Type]; ok {
				repos[i] = c.Collect(ctx, src)
				return nil
			}
			if src.Type != "local" {
				// A forge source with no collector would otherwise be read as
				// an empty local directory, every file missing.
				er := models.NewErrorResult(src.Slug, now, "collection_error", fmt.Sprintf("no collector for source type %q", src.Type))
				collErrs[i] = &er
				return nil
			}
			repo := fsc.Collect(src)
			if gctx := gitc.Collect(src); gctx != nil {
				repo.Git = gctx
			}
			repos[i] = repo
			return nil
		})
	}
	_ = g.Wait()

	// Preserve source order: successful repos first (in order), then the
	// collection errors appended after — matching the Python ordering.
	outRepos := make([]*models.NormalizedRepository, 0, len(sources))
	var collErrors []models.RepoResult
	for i := range sources {
		switch {
		case collErrs[i] != nil:
			collErrors = append(collErrors, *collErrs[i])
		case repos[i] != nil:
			outRepos = append(outRepos, repos[i])
		}
	}
	return outRepos, collErrors
}

// mergeCollectionErrors appends synthetic error results and recomputes counts,
// mirroring cli.py:185-203.
func mergeCollectionErrors(run models.RunResult, collErrors []models.RepoResult) models.RunResult {
	all := append(run.Repos, collErrors...)
	passed := 0
	for _, rr := range all {
		if !hasFailOrError(rr) {
			passed++
		}
	}
	run.Repos = all
	run.TotalRepos = len(all)
	run.Passed = passed
	run.Failed = len(all) - passed
	return run
}

func hasFailOrError(rr models.RepoResult) bool {
	for _, c := range rr.Results {
		if c.Status == models.StatusFail || c.Status == models.StatusError {
			return true
		}
	}
	return false
}

func formatHasJSON(f string) bool  { return f == "json" || f == "both" }
func formatHasTable(f string) bool { return f == "table" || f == "both" }

// mapError prints the error in the Python-equivalent form and returns exit 2.
func mapError(stderr io.Writer, err error) int {
	var ce *config.ConfigError
	var ae *config.AuthError
	var re *config.RateLimitError
	switch {
	case errors.As(err, &ce):
		fmt.Fprintf(stderr, "Error: %s\n", ce.Error())
	case errors.As(err, &ae):
		fmt.Fprintf(stderr, "Auth error: %s\n", ae.Error())
	case errors.As(err, &re):
		fmt.Fprintf(stderr, "%s\n", re.Error())
	default:
		fmt.Fprintf(stderr, "Unexpected error: %T: %v\n", err, err)
	}
	return 2
}

// needsPlatform reports whether any enabled, not globally ignored check needs
// the platform layer. Collecting it costs extra API calls per repo, so it is
// skipped unless a policy asks for it.
func needsPlatform(pol *models.Policy, registry *checks.Registry, ignore []string) bool {
	ignored := make(map[string]bool, len(ignore))
	for _, id := range ignore {
		ignored[id] = true
	}
	for _, def := range pol.Checks {
		if !def.Enabled || ignored[def.ID] {
			continue
		}
		if c, ok := registry.Get(def.ID); ok && c.Layer() == checks.LayerPlatform {
			return true
		}
	}
	return false
}
