package runner

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
)

// rateLimitWatch counts the API requests a forge refused under its rate limit
// during the scan, and when each limit lifts. For GitHub it reads go-github's
// errors, not HTTP responses: once a response shows no requests left,
// go-github refuses the rest of that limit's requests itself, without a
// request to see.
type rateLimitWatch struct {
	mu      sync.Mutex
	refused map[string]int
	reset   map[string]time.Time
}

// forgeRateLimit is an error from another forge's client that is a
// rate-limit refusal: which forge refused, and when its limit lifts (zero
// when it did not say).
type forgeRateLimit interface {
	RateLimit() (forge string, reset time.Time)
}

// observe records err if it is a rate-limit refusal: go-github's
// RateLimitError (the primary limit) or AbuseRateLimitError (a secondary
// limit), or a 429, which GitHub may also use for a secondary limit and
// go-github returns as a plain ErrorResponse.
func (w *rateLimitWatch) observe(err error) {
	forge := "GitHub"
	var reset time.Time
	var rl *github.RateLimitError
	var abuse *github.AbuseRateLimitError
	var er *github.ErrorResponse
	var other forgeRateLimit
	switch {
	case errors.As(err, &rl):
		reset = rl.Rate.Reset.Time
	case errors.As(err, &abuse):
		if abuse.RetryAfter != nil {
			reset = time.Now().Add(*abuse.RetryAfter)
		}
	case errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusTooManyRequests:
		if s, err := strconv.Atoi(er.Response.Header.Get("Retry-After")); err == nil {
			reset = time.Now().Add(time.Duration(s) * time.Second)
		}
	case errors.As(err, &other):
		forge, reset = other.RateLimit()
	default:
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refused == nil {
		w.refused, w.reset = map[string]int{}, map[string]time.Time{}
	}
	w.refused[forge]++
	if reset.After(w.reset[forge]) {
		w.reset[forge] = reset
	}
}

// summary returns the lines to print when requests were refused, one per
// forge in name order, or "".
func (w *rateLimitWatch) summary() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	forges := make([]string, 0, len(w.refused))
	for f := range w.refused {
		forges = append(forges, f)
	}
	sort.Strings(forges)
	lines := make([]string, 0, len(forges))
	for _, f := range forges {
		msg := fmt.Sprintf("%s refused %d request(s) under its API rate limit, so this scan is incomplete: "+
			"what they would have read is unknown, and findings issues they would have written were not", f, w.refused[f])
		if r := w.reset[f]; !r.IsZero() {
			msg += "; the limit resets at " + r.UTC().Format("2006-01-02 15:04 UTC")
		}
		lines = append(lines, msg)
	}
	return strings.Join(lines, "\n")
}
