// Package runner orchestrates a scan: config -> discovery -> collect -> evaluate
// -> output -> (issues). It returns a process exit code so the CLI stays thin and
// the pipeline is unit-testable.
package runner

import (
	"errors"
	"fmt"
	"io"
	"time"

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
}

// Scan runs the pipeline and returns the process exit code (0 pass, 1 failures, 2 error).
func Scan(stdout, stderr io.Writer, opts Options) int {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return mapError(stderr, err)
	}
	pol, err := policy.Load(cfg.Policy.Base)
	if err != nil {
		return mapError(stderr, err)
	}
	eng := engine.New(pol, checks.BuildDefault(), cfg.Policy.Ignore, cfg.Policy.RepoIgnores)

	sources, err := discover(cfg)
	if err != nil {
		return mapError(stderr, err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(stderr, "No repositories discovered. Check your scope config.")
		return 2
	}

	now := time.Now().UTC()
	repos, collErrors := collectAll(sources, now)
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

	if run.Failed > 0 {
		return 1
	}
	return 0
}

func discover(cfg *config.Config) ([]source.Repo, error) {
	var sources []source.Repo
	if cfg.Scope.GitHub != nil {
		// TODO(#15/#16): GitHub discovery + collector lands in step 8; guard until then.
		return nil, config.NewConfigError("GitHub scope not yet implemented in the Go port")
	}
	if cfg.Scope.Local != nil && len(cfg.Scope.Local.Paths) > 0 {
		sources = append(sources, discovery.Local{Paths: cfg.Scope.Local.Paths}.Discover()...)
	}
	return sources, nil
}

func collectAll(sources []source.Repo, now time.Time) ([]*models.NormalizedRepository, []models.RepoResult) {
	fsc := collectors.Filesystem{}
	gitc := collectors.NewGit()
	var repos []*models.NormalizedRepository
	var collErrors []models.RepoResult
	for _, src := range sources {
		repo := fsc.Collect(src)
		if g := gitc.Collect(src); g != nil {
			repo.Git = g
		}
		repos = append(repos, repo)
	}
	return repos, collErrors
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
