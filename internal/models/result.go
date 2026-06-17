package models

import "time"

// CheckStatus is the outcome of evaluating a single check against a repo.
type CheckStatus string

const (
	StatusPass  CheckStatus = "pass"
	StatusFail  CheckStatus = "fail"
	StatusSkip  CheckStatus = "skip"
	StatusError CheckStatus = "error"
)

// CheckResult is the outcome of one check on one repo.
type CheckResult struct {
	CheckID  string      `json:"check_id"`
	Status   CheckStatus `json:"status"`
	Severity Severity    `json:"severity"`
	Message  string      `json:"message,omitempty"`
}

// RepoResult aggregates all check results for a single repo plus its score.
type RepoResult struct {
	Slug      string        `json:"slug"`
	Timestamp time.Time     `json:"timestamp"`
	Score     float64       `json:"score"`
	Results   []CheckResult `json:"results"`
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
