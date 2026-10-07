// Package runner orchestrates a scan: config -> discovery -> collect -> evaluate
// -> output -> (issues). It returns a process exit code so the CLI stays thin and
// the pipeline is unit-testable.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
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
}

// Scan runs the pipeline and returns the process exit code (0 pass, 1 failures, 2 error).
func Scan(stdout, stderr io.Writer, opts Options) int {
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
	eng := engine.New(pol, registry, cfg.Policy.Ignore, cfg.Policy.RepoIgnores)
	platform := needsPlatform(pol, registry, cfg.Policy.Ignore)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	sources, client, err := discover(ctx, cfg)
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

	now := time.Now().UTC()
	repos, collErrors := collectAll(ctx, sources, client, platform, now)
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

	if opts.OpenIssues {
		if code := openIssues(ctx, stderr, cfg, client, sources, run, opts.DryRun); code != 0 {
			return code
		}
	}

	// Coverage is gated independently of posture: a repo whose evidence could not
	// be read must not pass on the strength of the few checks that did resolve.
	if opts.MinCoverage != nil {
		var under []string
		for _, rr := range run.Repos {
			if float64(rr.Coverage) < *opts.MinCoverage {
				under = append(under, fmt.Sprintf("%s (%.0f%%)", rr.Slug, float64(rr.Coverage)*100))
			}
		}
		if len(under) > 0 {
			fmt.Fprintf(stderr, "%d repo(s) below --min-coverage %.0f%%: %s\n",
				len(under), *opts.MinCoverage*100, strings.Join(under, ", "))
			return 1
		}
	}

	if opts.FailUnder != nil {
		var below []string
		for _, rr := range run.Repos {
			posture, ok := rr.Posture()
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
		if len(below) > 0 {
			fmt.Fprintf(stderr, "%d repo(s) below --fail-under %.2f: %s\n",
				len(below), *opts.FailUnder, strings.Join(below, ", "))
			return 1
		}
		return 0
	}

	if run.Failed > 0 {
		return 1
	}
	return 0
}

// openIssues opens/updates findings issues for GitHub repos. Returns exit 2 only
// when the required token is missing; per-repo failures are logged, not fatal.
func openIssues(ctx context.Context, stderr io.Writer, cfg *config.Config, client *github.Client, sources []source.Repo, run models.RunResult, dryRun bool) int {
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
		bySlug[s.Slug] = s
	}
	for _, rr := range run.Repos {
		s, ok := bySlug[rr.Slug]
		repo, isGH := s.GitHubRepo.(*github.Repository)
		if !ok || !isGH || repo == nil {
			slog.Warn("cannot open issue: no GitHub repo reference", "slug", rr.Slug)
			continue
		}
		if err := action.Run(ctx, rr, repo.GetOwner().GetLogin(), repo.GetName()); err != nil {
			slog.Warn("failed to open/update issue", "slug", rr.Slug, "err", err)
		}
	}
	return 0
}

// privacyOptions resolves the effective privacy guard settings: the
// --public-context flag (opts) overrides config's privacy.public_context, and
// the mode comes from config (default redact). The mode was already validated
// by config.Load, so ParseMode cannot fail here.
func privacyOptions(cfg *config.Config, opts Options) privacy.Options {
	public := false
	modeStr := ""
	if cfg.Privacy != nil {
		public = cfg.Privacy.PublicContext
		modeStr = cfg.Privacy.PrivateRepos
	}
	if opts.PublicContext != nil {
		public = *opts.PublicContext
	}
	mode, _ := privacy.ParseMode(modeStr)
	return privacy.Options{PublicContext: public, Mode: mode}
}

// guardStderr extends the privacy guard to stderr: when it is active, both the
// returned writer and the default slog logger redact private/internal slugs.
// privacy.Apply covers the results view; this covers everything else that
// reaches the log — issue-opening and collection log lines, and the runner's
// own messages such as the --fail-under list, which are built from the
// unredacted run. The returned func restores the previous default logger.
func guardStderr(stderr io.Writer, sources []source.Repo, cfg *config.Config, opts Options) (io.Writer, func()) {
	red := privacy.NewRedactor(repoVisibility(sources), privacyOptions(cfg, opts))
	if red == nil {
		return stderr, func() {}
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(red.Handler(prev.Handler())))
	return red.Writer(stderr), func() { slog.SetDefault(prev) }
}

// repoVisibility maps each GitHub source's slug to its visibility
// ("public"|"private"|"internal"), the input the privacy guard uses to decide
// what to protect. Built from sources (not results) so it also covers repos
// that failed collection. Local/non-GitHub sources are omitted (treated as
// public). Some list endpoints omit Visibility, so fall back to Private.
func repoVisibility(sources []source.Repo) map[string]string {
	vis := make(map[string]string, len(sources))
	for _, s := range sources {
		r, ok := s.GitHubRepo.(*github.Repository)
		if !ok || r == nil {
			continue
		}
		v := r.GetVisibility()
		if v == "" {
			if r.GetPrivate() {
				v = "private"
			} else {
				v = "public"
			}
		}
		vis[s.Slug] = v
	}
	return vis
}

// newGitHubClient returns an API client for token. GITHUB_API_URL, when set,
// is the API root to use instead of api.github.com — GitHub Actions sets it on
// every runner, to the Enterprise Server API on GHES.
func newGitHubClient(token string) (*github.Client, error) {
	client := github.NewClient(nil).WithAuthToken(token)
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

func discover(ctx context.Context, cfg *config.Config) ([]source.Repo, *github.Client, error) {
	var sources []source.Repo
	var client *github.Client
	if cfg.Scope.GitHub != nil {
		token := strings.TrimSpace(os.Getenv(cfg.Scope.GitHub.TokenEnv))
		if token == "" {
			return nil, nil, config.NewAuthError(
				"GitHub token not found in environment variable '%s'. "+
					"Set it in your environment and re-run the scan.", cfg.Scope.GitHub.TokenEnv)
		}
		c, err := newGitHubClient(token)
		if err != nil {
			return nil, nil, err
		}
		client = c
		gh := discovery.GitHub{
			Client:  client,
			Cfg:     *cfg.Scope.GitHub,
			Include: cfg.Scope.Include,
			Exclude: cfg.Scope.Exclude,
		}
		ghSources, err := gh.Discover(ctx)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, ghSources...)
	}
	if cfg.Scope.Local != nil && len(cfg.Scope.Local.Paths) > 0 {
		sources = append(sources, discovery.Local{Paths: cfg.Scope.Local.Paths}.Discover()...)
	}
	return sources, client, nil
}

// collectConcurrency bounds parallel collection (I/O-bound: GitHub API + git).
const collectConcurrency = 8

// collectAll collects every source concurrently (bounded) while preserving source
// order in the output — so the console/JSON ordering is identical to a serial run.
func collectAll(ctx context.Context, sources []source.Repo, client *github.Client, platform bool, now time.Time) ([]*models.NormalizedRepository, []models.RepoResult) {
	fsc := collectors.Filesystem{}
	gitc := collectors.NewGit()
	var ghc *collectors.GitHubAPI
	if client != nil {
		c := collectors.NewGitHubAPI(client)
		c.Platform = platform
		ghc = &c
	}

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
			if src.Type == "github" {
				repos[i] = ghc.Collect(ctx, src)
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
