// Package engine evaluates repositories against a policy and scores them.
package engine

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/baselinerhq/baseliner/internal/checks"
	"github.com/baselinerhq/baseliner/internal/models"
)

// Engine evaluates repos against a policy using a check registry.
type Engine struct {
	Policy       *models.Policy
	Registry     *checks.Registry
	GlobalIgnore map[string]bool
	RepoIgnores  map[string][]string
}

// New builds an engine, normalizing ignore inputs into sets.
func New(policy *models.Policy, registry *checks.Registry, globalIgnore []string, repoIgnores map[string][]string) *Engine {
	gi := make(map[string]bool, len(globalIgnore))
	for _, id := range globalIgnore {
		gi[id] = true
	}
	return &Engine{Policy: policy, Registry: registry, GlobalIgnore: gi, RepoIgnores: repoIgnores}
}

// Run evaluates a single repo and returns its scored result.
func (e *Engine) Run(repo *models.NormalizedRepository, now time.Time) models.RepoResult {
	repoIgnore := make(map[string]bool)
	for _, id := range e.RepoIgnores[repo.Slug] {
		repoIgnore[id] = true
	}

	var results []models.CheckResult
	for _, def := range e.Policy.Checks {
		if !def.Enabled {
			continue
		}
		if e.GlobalIgnore[def.ID] || repoIgnore[def.ID] {
			slog.Debug("skipping ignored check", "check", def.ID, "repo", repo.Slug)
			continue
		}
		c, ok := e.Registry.Get(def.ID)
		if !ok {
			slog.Warn("unknown check id in policy — skipping", "check", def.ID)
			continue
		}
		res := checks.Evaluate(c, repo)
		res.Severity = def.Severity // policy severity overrides the check's default
		results = append(results, res)
	}

	return models.RepoResult{
		Slug:      repo.Slug,
		Timestamp: now,
		Score:     computeScore(results),
		Results:   results,
	}
}

// computeScore is the severity-weighted pass ratio. Skipped checks are
// excluded; an all-skip (zero total weight) result scores 1.0.
func computeScore(results []models.CheckResult) float64 {
	totalWeight, passedWeight := 0, 0
	for _, r := range results {
		if r.Status == models.StatusSkip {
			continue
		}
		w := r.Severity.Weight()
		totalWeight += w
		if r.Status == models.StatusPass {
			passedWeight += w
		}
	}
	if totalWeight == 0 {
		return 1.0
	}
	return math.Round(float64(passedWeight)/float64(totalWeight)*10000) / 10000
}

// RunBatch evaluates many repos and aggregates pass/fail counts.
func (e *Engine) RunBatch(repos []*models.NormalizedRepository, now time.Time) models.RunResult {
	repoResults := make([]models.RepoResult, 0, len(repos))
	for _, repo := range repos {
		repoResults = append(repoResults, e.Run(repo, now))
	}

	passed := 0
	for _, rr := range repoResults {
		if !hasFailureOrError(rr) {
			passed++
		}
	}

	return models.RunResult{
		RunID:      newRunID(),
		Timestamp:  now,
		TotalRepos: len(repoResults),
		Passed:     passed,
		Failed:     len(repoResults) - passed,
		Repos:      repoResults,
	}
}

func hasFailureOrError(rr models.RepoResult) bool {
	for _, r := range rr.Results {
		if r.Status == models.StatusFail || r.Status == models.StatusError {
			return true
		}
	}
	return false
}

// newRunID returns a random UUIDv4 string (avoids an external dependency).
func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
