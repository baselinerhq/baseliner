package models

import (
	"strconv"
	"strings"
	"time"
)

// Score is a repo's severity-weighted pass ratio. It marshals like Python's
// pydantic float: integral values keep a decimal point (`1.0`, `0.0`) rather
// than collapsing to `1`/`0` as Go's encoding/json does by default.
type Score float64

// MarshalJSON renders the score with the shortest round-trip representation,
// appending ".0" for integral values to match pydantic's float serialization.
func (s Score) MarshalJSON() ([]byte, error) {
	str := strconv.FormatFloat(float64(s), 'g', -1, 64)
	if !strings.ContainsAny(str, ".eE") {
		str += ".0"
	}
	return []byte(str), nil
}

// CheckStatus is the outcome of evaluating a single check against a repo.
type CheckStatus string

const (
	StatusPass CheckStatus = "pass"
	StatusFail CheckStatus = "fail"
	// StatusSkip means the check does not apply to this repo. It is excluded
	// from both posture and coverage.
	StatusSkip CheckStatus = "skip"
	// StatusUnknown means the check applies but could not be observed (e.g. the
	// required context was unavailable). It reduces coverage and never counts
	// toward posture — absence of evidence is not compliance.
	StatusUnknown CheckStatus = "unknown"
	StatusError   CheckStatus = "error"
)

// CheckResult is the outcome of one check on one repo.
// Message is a pointer so it serializes as JSON null (not omitted) when absent,
// matching the Python pydantic output.
type CheckResult struct {
	CheckID  string      `json:"check_id"`
	Status   CheckStatus `json:"status"`
	Severity Severity    `json:"severity"`
	Message  *string     `json:"message"`
	// PolicyInfo/PolicyURL carry optional context from the check definition —
	// why the check matters and a link to the governing standard — so a finding
	// is actionable. Omitted from JSON when unset.
	PolicyInfo string `json:"policy_info,omitempty"`
	PolicyURL  string `json:"policy_url,omitempty"`
}

// RepoResult aggregates all check results for a single repo plus its score.
//
// Score (posture) and Coverage are deliberately separate. Posture grades only
// what was conclusively observed; coverage reports how much of the applicable
// baseline could be observed at all. Collapsing them into one number would let
// missing evidence read as compliance.
//
// Score is a pointer so it serializes as JSON null when nothing conclusive was
// observed — never as 1.0, which is what an all-unobserved repo used to score.
type RepoResult struct {
	Slug      string        `json:"slug"`
	Timestamp time.Time     `json:"timestamp"`
	Score     *Score        `json:"score"`
	Coverage  Score         `json:"coverage"`
	Results   []CheckResult `json:"results"`
}

// ScorePtr returns a pointer to s. RepoResult.Score is nullable to distinguish
// "not assessed" from a real score, so constructing one needs an addressable
// value.
func ScorePtr(s Score) *Score { return &s }

// Posture returns the repo's severity-weighted pass ratio and whether it is
// defined. It is undefined when no check produced a conclusive pass/fail.
func (r RepoResult) Posture() (float64, bool) {
	if r.Score == nil {
		return 0, false
	}
	return float64(*r.Score), true
}

// NewErrorResult builds a RepoResult representing a failure to collect or
// evaluate a single repo: posture undefined (null) and coverage 0, with one
// critical ERROR check. A collection failure means the repo was not assessed,
// which is distinct from being assessed and found non-compliant — so it reports
// no posture rather than a score of 0.
func NewErrorResult(slug string, ts time.Time, checkID, message string) RepoResult {
	return RepoResult{
		Slug:      slug,
		Timestamp: ts,
		Score:     nil,
		Coverage:  0,
		Results: []CheckResult{{
			CheckID:  checkID,
			Status:   StatusError,
			Severity: SeverityCritical,
			Message:  &message,
		}},
	}
}

// PrivacyNote records that private/internal repos received privacy treatment in
// a public-context scan. It is attached only to the disclosure-facing view, so
// it is absent (omitempty) from normal output.
type PrivacyNote struct {
	Mode  string `json:"mode"`  // "redact" | "exclude"
	Count int    `json:"count"` // number of private/internal repos affected
}

// RunResult is the top-level output of a scan over a fleet of repos.
type RunResult struct {
	RunID      string       `json:"run_id"`
	Timestamp  time.Time    `json:"timestamp"`
	TotalRepos int          `json:"total_repos"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Repos      []RepoResult `json:"repos"`
	Privacy    *PrivacyNote `json:"privacy,omitempty"`
}
