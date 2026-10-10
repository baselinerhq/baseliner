// Package gitea is a minimal read-only client for the Gitea API (v1), which
// Forgejo and Codeberg serve too: the few endpoints baseliner reads to
// discover and collect an organisation's or a user's repos. Errors name the
// endpoint with "(repo)" in place of the owner and name.
package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	forge           = "Gitea" // names this client in error text
	maxJSONBytes    = 16 << 20
	maxMessageBytes = 200
	pageSize        = 50 // Gitea's default maximum
)

// Client calls one Gitea or Forgejo instance's API with one token.
type Client struct {
	api   *url.URL // the instance root plus /api/v1/
	token string
	HTTP  *http.Client
}

// New returns a client for the instance at baseURL (its root, such as
// https://codeberg.org) that authenticates with token.
func New(baseURL, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/") + "/api/v1/")
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("gitea: base URL must be an absolute http(s) URL")
	}
	c := &Client{api: u, token: token}
	// A redirect is followed only within this API: the token goes with it.
	c.HTTP = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if req.URL.Scheme != c.api.Scheme || req.URL.Host != c.api.Host ||
			!strings.HasPrefix(path.Clean(req.URL.Path)+"/", c.api.Path) {
			return errors.New("redirect points outside the API; not followed")
		}
		return nil
	}}
	return c, nil
}

// Owner is a repo's owning user or organisation.
type Owner struct {
	Login string `json:"login"`
	// Visibility is the owner's: "public", "limited" (signed-in users) or
	// "private". A repo of a limited or private owner is not public, whatever
	// its own flags say.
	Visibility string `json:"visibility"`
}

// Repo is the part of a repo baseliner reads.
type Repo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Owner         Owner  `json:"owner"`
	Private       bool   `json:"private"`
	Internal      bool   `json:"internal"`
	Archived      bool   `json:"archived"`
	Empty         bool   `json:"empty"`
	DefaultBranch string `json:"default_branch"`
}

// Entry is one entry of a directory listing.
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file", "dir", "symlink" or "submodule"
}

// Error is a failed API call. Its text names the method, the endpoint with
// "(repo)" in place of the owner and name, the status and at most
// maxMessageBytes of the server's message, on one line.
type Error struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Gitea %s %s: %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("Gitea %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// RateLimitError is a request refused under the instance's rate limit (429).
type RateLimitError struct {
	Err   *Error
	Reset time.Time // zero when the instance did not say
}

func (e *RateLimitError) Error() string { return e.Err.Error() }
func (e *RateLimitError) Unwrap() error { return e.Err }

// RateLimit reports the forge and when its limit lifts.
func (e *RateLimitError) RateLimit() (string, time.Time) { return "Gitea", e.Reset }

// Status returns err's HTTP status, or 0 when it is not an API error.
func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// Visibility returns r's effective visibility: "private", "internal" or
// "public", the most restrictive of the repo's flags and its owner's
// visibility. A repo with private=false in a limited organisation is not
// public. An owner visibility it does not know is private.
func Visibility(r Repo) string {
	switch {
	case r.Private:
		return "private"
	}
	owner := strings.ToLower(strings.TrimSpace(r.Owner.Visibility))
	switch owner {
	case "", "public":
	case "limited":
		return "internal"
	default: // "private", or anything unrecognised
		return "private"
	}
	if r.Internal {
		return "internal"
	}
	return "public"
}

// Repos lists an organisation's ("org") or a user's ("user") repos. It
// reports false when there were more than maxPages pages. An instance can cap
// a page below the limit asked for (MAX_RESPONSE_ITEMS), so only an empty
// page ends the listing.
func (c *Client) Repos(ctx context.Context, kind, name string, maxPages int) ([]Repo, bool, error) {
	base := "orgs/"
	if kind == "user" {
		base = "users/"
	}
	var out []Repo
	for page := 1; page <= maxPages; page++ {
		var batch []Repo
		q := url.Values{"limit": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
		if err := c.getJSON(ctx, base+url.PathEscape(name)+"/repos", q, "/"+base+"(owner)/repos", &batch); err != nil {
			return nil, false, err
		}
		if len(batch) == 0 {
			return out, true, nil
		}
		out = append(out, batch...)
	}
	return out, false, nil
}

// AnonymousVisible reports whether owner/repo can be read without a token.
// An instance can require sign-in to view anything (REQUIRE_SIGNIN_VIEW):
// its API then reports public repos as public to a signed-in caller, though
// nobody outside can see them. Visible means a 200 whose body is that repo,
// so a sign-in page that answers 200 is not. A refusal (401, 403, 404) or a
// redirect, which is not followed, is not visible. Any other answer is an
// error, which names no repo.
func (c *Client) AnonymousVisible(ctx context.Context, owner, repo string) (bool, error) {
	u := c.endpoint(repoPath(owner, repo), nil)
	u.User = nil // credentials in the base URL would be sent as Basic auth
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, errors.New("building the request failed")
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Transport:     c.HTTP.Transport,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		// Not the *url.Error itself: it quotes the URL, which names the repo.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
		var got Repo
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&got) != nil {
			return false, nil
		}
		return strings.EqualFold(got.Name, repo), nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound, resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, nil
	}
	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// Contents lists the entries directly under dir ("" for the root) at ref.
// isDir is false when the path is a file, so there is no directory there.
func (c *Client) Contents(ctx context.Context, owner, repo, ref, dir string) (entries []Entry, isDir bool, err error) {
	var raw json.RawMessage
	p := repoPath(owner, repo) + "/contents/" + escapePath(dir)
	if err := c.getJSON(ctx, p, url.Values{"ref": {ref}}, "/repos/(repo)/contents/"+dir, &raw); err != nil {
		return nil, false, err
	}
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '[' {
		return nil, false, nil
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false, fmt.Errorf("%s GET /repos/(repo)/contents/%s: decoding the response: %w", forge, dir, err)
	}
	return entries, true, nil
}

// RawFile returns at most limit bytes of the file at path, at ref.
func (c *Client) RawFile(ctx context.Context, owner, repo, ref, file string, limit int64) ([]byte, error) {
	u := c.endpoint(repoPath(owner, repo)+"/raw/"+escapePath(file), url.Values{"ref": {ref}})
	resp, err := c.do(ctx, u, "/repos/(repo)/raw/(path)")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Branches returns up to max branch names.
func (c *Client) Branches(ctx context.Context, owner, repo string, max int) ([]string, error) {
	var out []string
	for page := 1; len(out) < max; page++ {
		var batch []struct {
			Name string `json:"name"`
		}
		q := url.Values{"limit": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
		if err := c.getJSON(ctx, repoPath(owner, repo)+"/branches", q, "/repos/(repo)/branches", &batch); err != nil {
			return out, err
		}
		for _, b := range batch {
			if len(out) < max {
				out = append(out, b.Name)
			}
		}
		if len(batch) == 0 {
			break
		}
	}
	return out, nil
}

// LastCommit returns the time of the latest commit on branch.
func (c *Client) LastCommit(ctx context.Context, owner, repo, branch string) (time.Time, error) {
	var b struct {
		Commit struct {
			Timestamp time.Time `json:"timestamp"`
		} `json:"commit"`
	}
	err := c.getJSON(ctx, repoPath(owner, repo)+"/branches/"+url.PathEscape(branch), nil, "/repos/(repo)/branches/(branch)", &b)
	return b.Commit.Timestamp, err
}

func repoPath(owner, repo string) string {
	return "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func (c *Client) endpoint(escPath string, q url.Values) *url.URL {
	u := *c.api
	u.RawPath = c.api.EscapedPath() + escPath
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.RawQuery = q.Encode()
	return &u
}

func (c *Client) getJSON(ctx context.Context, escPath string, q url.Values, shown string, v any) error {
	resp, err := c.do(ctx, c.endpoint(escPath, q), shown)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(v); err != nil {
		return fmt.Errorf("%s GET %s: decoding the response: %w", forge, shown, err)
	}
	return nil
}

// do sends a GET and returns the response when it succeeded. shown replaces
// the request path in an error, so it never names the owner or repo.
func (c *Client) do(ctx context.Context, u *url.URL, shown string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s GET %s: %w", forge, shown, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	e := &Error{Method: http.MethodGet, Path: shown, Status: resp.StatusCode, Message: message(resp.Body)}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Err: e, Reset: reset(resp.Header)}
	}
	return nil, e
}

// message returns the server's message on one line, bounded. Gitea's error
// body also carries an "errors" list, which can quote a path; only the
// message is kept.
func message(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 64<<10))
	var v struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &v)
	text := strings.Join(strings.Fields(v.Message), " ")
	if len(text) > maxMessageBytes {
		text = strings.ToValidUTF8(text[:maxMessageBytes], "") + "…"
	}
	return text
}

// reset reads when a rate limit lifts from Retry-After (seconds); zero when
// it is not given.
func reset(h http.Header) time.Time {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s >= 0 {
		return time.Now().Add(time.Duration(s) * time.Second)
	}
	return time.Time{}
}
