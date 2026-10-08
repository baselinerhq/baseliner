// Package engine evaluates repositories against a policy and scores them.
package engine

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
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
	// IgnoreWhen waives checks by repo visibility.
	IgnoreWhen []VisibilityIgnore
	// WaivableChecks are the checks a repo's own waivers may cover; nil
	// means repo waivers do not apply.
	WaivableChecks map[string]bool
}

// New builds an engine, normalizing ignore inputs into sets.
func New(policy *models.Policy, registry *checks.Registry, globalIgnore []string, repoIgnores map[string][]string) *Engine {
	gi := make(map[string]bool, len(globalIgnore))
	for _, id := range globalIgnore {
		gi[id] = true
	}
	return &Engine{Policy: policy, Registry: registry, GlobalIgnore: gi, RepoIgnores: repoIgnores}
}

// VisibilityIgnore waives Checks on repos whose visibility is one of
// Visibility.
type VisibilityIgnore struct {
	Visibility []string
	Checks     []string
}

// Run evaluates a single repo and returns its scored result.
func (e *Engine) Run(repo *models.NormalizedRepository, now time.Time) models.RepoResult {
	repoIgnore := make(map[string]bool)
	for _, id := range e.RepoIgnores[repo.Slug] {
		repoIgnore[id] = true
	}
	for _, rule := range e.IgnoreWhen {
		// Rule values are validated non-empty, so a repo with no visibility,
		// such as a local checkout, matches none.
		if slices.Contains(rule.Visibility, repo.Visibility) {
			for _, id := range rule.Checks {
				repoIgnore[id] = true
			}
		}
	}

	waivers := e.applicableWaivers(repo, now)

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
		var res models.CheckResult
		if w, ok := waivers[def.ID]; ok {
			res = models.CheckResult{CheckID: def.ID, Status: models.StatusWaived, Message: &w}
		} else {
			res = checks.Evaluate(c, repo)
		}
		res.Severity = def.Severity // policy severity overrides the check's default
		res.PolicyInfo = def.PolicyInfo
		res.PolicyURL = def.PolicyURL
		results = append(results, res)
	}

	posture, coverage := computeScores(results)
	return models.RepoResult{
		Slug:      repo.Slug,
		Timestamp: now,
		Score:     posture,
		Coverage:  coverage,
		Results:   results,
	}
}

// applicableWaivers returns the repo's waivers that apply, keyed by check, as
// the message the waived result carries. A waiver applies when the policy
// allows repos to waive that check and it has not expired; any other is
// logged and the check runs as usual.
func (e *Engine) applicableWaivers(repo *models.NormalizedRepository, now time.Time) map[string]string {
	out := map[string]string{}
	for _, w := range repo.Waivers {
		switch {
		case e.WaivableChecks == nil:
			slog.Warn("repo waiver not applied: policy.repo_waivers is not set", "repo", repo.Slug, "check", w.Check)
		case !e.WaivableChecks[w.Check]:
			slog.Warn("repo waiver not applied: policy.repo_waivers.allow does not list the check", "repo", repo.Slug, "check", w.Check)
		case !w.Active(now):
			slog.Warn("repo waiver not applied: expired", "repo", repo.Slug, "check", w.Check, "until", w.Until.Format("2006-01-02"))
		default:
			msg := "waived by the repo: " + w.Reason
			if w.Until != nil {
				msg += " (until " + w.Until.Format("2006-01-02") + ")"
			}
			out[w.Check] = msg
		}
	}
	return out
}

// runSafe evaluates one repo, converting a panic in any check into an
// engine_error result (score 0) so a single misbehaving check cannot abort the
// whole batch — mirroring the Python engine's per-repo try/except.
func (e *Engine) runSafe(repo *models.NormalizedRepository, now time.Time) (rr models.RepoResult) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("unhandled error evaluating repo", "repo", repo.Slug, "panic", p)
			rr = models.NewErrorResult(repo.Slug, now, "engine_error", fmt.Sprintf("%v", p))
		}
	}()
	return e.Run(repo, now)
}

// computeScores returns the severity-weighted posture and coverage.
//
//	posture  = passed / (passed + failed)
//	coverage = (passed + failed) / (passed + failed + unobserved)
//
// StatusSkip (not applicable) is excluded from both. StatusUnknown and
// StatusError are unobserved: they reduce coverage and never raise posture.
// Posture is nil when nothing conclusive was observed — the old behavior
// returned 1.0 there, which reported missing evidence as perfect compliance.
func computeScores(results []models.CheckResult) (*models.Score, models.Score) {
	conclusiveWeight, passedWeight, unobservedWeight := 0, 0, 0
	for _, r := range results {
		w := r.Severity.Weight()
		switch r.Status {
		case models.StatusPass:
			conclusiveWeight += w
			passedWeight += w
		case models.StatusFail:
			conclusiveWeight += w
		case models.StatusUnknown, models.StatusError:
			unobservedWeight += w
		case models.StatusSkip, models.StatusWaived:
			// not applicable — out of both ratios
		}
	}

	applicableWeight := conclusiveWeight + unobservedWeight
	var coverage models.Score
	if applicableWeight > 0 {
		coverage = models.Score(round4(float64(conclusiveWeight) / float64(applicableWeight)))
	}

	if conclusiveWeight == 0 {
		return nil, coverage
	}
	posture := models.Score(round4(float64(passedWeight) / float64(conclusiveWeight)))
	return &posture, coverage
}

// round4 rounds to 4 decimal places using round-half-to-even on the exact
// IEEE-754 value, matching Python's round(x, 4). Go's math.Round rounds half
// away from zero, which diverges at exact-half boundaries (e.g. 17/32 → Go
// 0.5313 vs Python 0.5312); strconv's correctly-rounded formatter does not.
func round4(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 4, 64), 64)
	return v
}

// RunBatch evaluates many repos and aggregates pass/fail counts.
func (e *Engine) RunBatch(repos []*models.NormalizedRepository, now time.Time) models.RunResult {
	repoResults := make([]models.RepoResult, 0, len(repos))
	for _, repo := range repos {
		repoResults = append(repoResults, e.runSafe(repo, now))
	}

	passed := 0
	for _, rr := range repoResults {
		if !repoFailed(rr) {
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

// repoFailed reports whether a repo counts against the default gate: any failing
// or errored check, or no conclusive result at all. The latter matters because a
// repo nothing could be observed on must not pass by default — compliance has to
// be demonstrated, not assumed from silence. Partial gaps are visible as
// coverage and gated explicitly with --min-coverage.
func repoFailed(rr models.RepoResult) bool {
	for _, r := range rr.Results {
		if r.Status == models.StatusFail || r.Status == models.StatusError {
			return true
		}
	}
	_, assessed := rr.Posture()
	return !assessed
}

// newRunID returns a random UUIDv4 string (avoids an external dependency).
func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
