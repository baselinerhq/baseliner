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
	"github.com/baselinerhq/baseliner/internal/source"
)

// Options are the resolved scan flags.
type Options struct {
	ConfigPath string
	OutputFile string
	Format     string // "json" | "table" | "both"
	OpenIssues bool
	DryRun     bool
	Quiet      bool
	// FailUnder, when set, replaces the default per-check gate: the scan exits 1
	// if any repo scores below the threshold (every repo must be >= it), else 0.
	FailUnder *float64
}

// Scan runs the pipeline and returns the process exit code (0 pass, 1 failures, 2 error).
func Scan(stdout, stderr io.Writer, opts Options) int {
	switch opts.Format {
	case "json", "table", "both":
	default:
		fmt.Fprintf(stderr, "invalid --format %q: must be json, table, or both\n", opts.Format)
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
	eng := engine.New(pol, checks.BuildDefault(), cfg.Policy.Ignore, cfg.Policy.RepoIgnores)

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

	now := time.Now().UTC()
	repos, collErrors := collectAll(ctx, sources, client, now)
	run := eng.RunBatch(repos, now)
	if len(collErrors) > 0 {
		run = mergeCollectionErrors(run, collErrors)
	}

	if formatHasJSON(opts.Format) {
		if err := output.WriteJSON(stdout, &run, opts.OutputFile); err != nil {
			fmt.Fprintf(stderr, "Error: could not write JSON output: %v\n", err)
			return 2
		}
	}
	if formatHasTable(opts.Format) && !opts.Quiet {
		output.PrintSummary(stdout, &run)
	}

	if opts.OpenIssues {
		if code := openIssues(ctx, stderr, cfg, client, sources, run, opts.DryRun); code != 0 {
			return code
		}
	}

	if opts.FailUnder != nil {
		var below []string
		for _, rr := range run.Repos {
			if float64(rr.Score) < *opts.FailUnder {
				below = append(below, fmt.Sprintf("%s (%.2f)", rr.Slug, float64(rr.Score)))
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
		client = github.NewClient(nil).WithAuthToken(token)
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
		client = github.NewClient(nil).WithAuthToken(token)
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
func collectAll(ctx context.Context, sources []source.Repo, client *github.Client, now time.Time) ([]*models.NormalizedRepository, []models.RepoResult) {
	fsc := collectors.Filesystem{}
	gitc := collectors.NewGit()
	var ghc *collectors.GitHubAPI
	if client != nil {
		c := collectors.NewGitHubAPI(client)
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
