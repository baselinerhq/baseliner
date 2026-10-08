package runner

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimitWatch records whether GitHub refused a request under a rate limit
// during the scan, and when the limit lifts. go-github does not retry: once
// one request is refused it fails the rest of that limit's requests itself,
// so the first refused response is the one to see, and the checks that
// needed the refused reads report unknown.
type rateLimitWatch struct {
	mu    sync.Mutex
	hit   bool
	reset time.Time
}

// transport wraps base so every response passes through the watch.
func (w *rateLimitWatch) transport(base http.RoundTripper) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err == nil {
			w.note(resp)
		}
		return resp, err
	})
}

// note records resp if it is a rate-limit refusal: a 403 or 429 with no
// requests remaining (the primary limit) or with Retry-After (a secondary
// limit).
func (w *rateLimitWatch) note(resp *http.Response) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	var reset time.Time
	switch {
	case resp.Header.Get("X-RateLimit-Remaining") == "0":
		if s, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			reset = time.Unix(s, 0)
		}
	case resp.Header.Get("Retry-After") != "":
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			reset = time.Now().Add(time.Duration(s) * time.Second)
		}
	default:
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hit = true
	if reset.After(w.reset) {
		w.reset = reset
	}
}

// summary returns the line to print when a limit was hit, or "".
func (w *rateLimitWatch) summary() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hit {
		return ""
	}
	msg := "GitHub refused requests under its API rate limit, so this scan is incomplete: checks that needed them are unknown"
	if !w.reset.IsZero() {
		msg += "; the limit resets at " + w.reset.UTC().Format("2006-01-02 15:04 UTC")
	}
	return msg
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
