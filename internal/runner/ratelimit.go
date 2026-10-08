package runner

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
)

// rateLimitWatch counts the API requests refused under a GitHub rate limit
// during the scan, and when the limit lifts. It reads go-github's errors, not
// HTTP responses: once a response shows no requests left, go-github refuses
// the rest of that limit's requests itself, without a request to see.
type rateLimitWatch struct {
	mu      sync.Mutex
	refused int
	reset   time.Time
}

// observe records err if it is a rate-limit refusal: go-github's
// RateLimitError (the primary limit) or AbuseRateLimitError (a secondary
// limit), or a 429, which GitHub may also use for a secondary limit and
// go-github returns as a plain ErrorResponse.
func (w *rateLimitWatch) observe(err error) {
	var reset time.Time
	var rl *github.RateLimitError
	var abuse *github.AbuseRateLimitError
	var er *github.ErrorResponse
	switch {
	case errors.As(err, &rl):
		reset = rl.Rate.Reset.Time
	case errors.As(err, &abuse):
		if abuse.RetryAfter != nil {
			reset = time.Now().Add(*abuse.RetryAfter)
		}
	case errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusTooManyRequests:
	default:
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refused++
	if reset.After(w.reset) {
		w.reset = reset
	}
}

// summary returns the line to print when requests were refused, or "".
func (w *rateLimitWatch) summary() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refused == 0 {
		return ""
	}
	msg := fmt.Sprintf("GitHub refused %d request(s) under its API rate limit, so this scan is incomplete: "+
		"what they would have read is unknown, and findings issues they would have written were not", w.refused)
	if !w.reset.IsZero() {
		msg += "; the limit resets at " + w.reset.UTC().Format("2006-01-02 15:04 UTC")
	}
	return msg
}
