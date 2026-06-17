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
	StatusPass  CheckStatus = "pass"
	StatusFail  CheckStatus = "fail"
	StatusSkip  CheckStatus = "skip"
	StatusError CheckStatus = "error"
)

// CheckResult is the outcome of one check on one repo.
// Message is a pointer so it serializes as JSON null (not omitted) when absent,
// matching the Python pydantic output.
type CheckResult struct {
	CheckID  string      `json:"check_id"`
	Status   CheckStatus `json:"status"`
	Severity Severity    `json:"severity"`
	Message  *string     `json:"message"`
}

// RepoResult aggregates all check results for a single repo plus its score.
type RepoResult struct {
	Slug      string        `json:"slug"`
	Timestamp time.Time     `json:"timestamp"`
	Score     Score         `json:"score"`
	Results   []CheckResult `json:"results"`
}

// NewErrorResult builds a RepoResult representing a failure to collect or
// evaluate a single repo: score 0 with one critical ERROR check. Mirrors the
// collection_error / engine_error results the Python CLI and engine emit so one
// bad repo degrades to a single error row instead of aborting the fleet scan.
func NewErrorResult(slug string, ts time.Time, checkID, message string) RepoResult {
	return RepoResult{
		Slug:      slug,
		Timestamp: ts,
		Score:     0,
		Results: []CheckResult{{
			CheckID:  checkID,
			Status:   StatusError,
			Severity: SeverityCritical,
			Message:  &message,
		}},
	}
}

// RunResult is the top-level output of a scan over a fleet of repos.
type RunResult struct {
	RunID      string       `json:"run_id"`
	Timestamp  time.Time    `json:"timestamp"`
	TotalRepos int          `json:"total_repos"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Repos      []RepoResult `json:"repos"`
}
