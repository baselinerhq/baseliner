// Package gitlab is a minimal read-only client for the GitLab REST API (v4):
// the few endpoints baseliner reads to discover and collect a group's
// projects. Every per-project request addresses the project by its numeric
// ID, so request URLs, and the errors that quote them, never carry a project
// path.
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxJSONBytes bounds a JSON response read into memory.
const maxJSONBytes = 16 << 20

// maxMessageBytes bounds the server message kept in an Error.
const maxMessageBytes = 200

// Client calls one GitLab instance's API with one token.
type Client struct {
	api   *url.URL // the instance root plus /api/v4/
	token string
	HTTP  *http.Client
}

// New returns a client for the instance at baseURL (its root, such as
// https://gitlab.com) that authenticates with token.
func New(baseURL, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/") + "/api/v4/")
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("gitlab: base URL must be an absolute http(s) URL")
	}
	return &Client{api: u, token: token, HTTP: http.DefaultClient}, nil
}

// Group is a GitLab group.
type Group struct {
	ID       int64  `json:"id"`
	FullPath string `json:"full_path"`
}

// Project is the part of a GitLab project baseliner reads. Display names are
// left out on purpose: only paths are used.
type Project struct {
	ID                int64      `json:"id"`
	Path              string     `json:"path"`
	PathWithNamespace string     `json:"path_with_namespace"`
	Visibility        string     `json:"visibility"`
	DefaultBranch     string     `json:"default_branch"`
	Archived          bool       `json:"archived"`
	EmptyRepo         bool       `json:"empty_repo"`
	LastActivityAt    *time.Time `json:"last_activity_at"`
}

// TreeEntry is one entry of a repository tree listing.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob", "tree" or "commit" (a submodule)
	Mode string `json:"mode"` // "120000" for a symlink
}

// Error is a failed API call. Its text holds the method, the request path
// (numeric IDs only, but for the group lookup), the status and at most
// maxMessageBytes of the server's message, on one line.
type Error struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitLab %s %s: %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("GitLab %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

// RateLimitError is a request GitLab refused under its rate limit (429).
type RateLimitError struct {
	Err   *Error
	Reset time.Time // zero when GitLab did not say
}

func (e *RateLimitError) Error() string { return e.Err.Error() }
func (e *RateLimitError) Unwrap() error { return e.Err }

// RateLimit reports the forge and when its limit lifts.
func (e *RateLimitError) RateLimit() (string, time.Time) { return "GitLab", e.Reset }

// Status returns err's HTTP status, or 0 when it is not an API error.
func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// IsAbsent reports whether err says the thing asked for does not exist: a
// 404 about a file or tree, not about the project itself, which would mean
// the project cannot be read.
func IsAbsent(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound && !strings.Contains(e.Message, "Project Not Found")
}

// Visibility returns p's visibility, lowercased: "public", "internal" or
// "private". Anything else, including none, is "private", so the privacy
// guard protects a project whose visibility it could not read.
func Visibility(p Project) string {
	switch v := strings.ToLower(strings.TrimSpace(p.Visibility)); v {
	case "public", "internal", "private":
		return v
	default:
		return "private"
	}
}

// Group looks up a group by its full path.
func (c *Client) Group(ctx context.Context, path string) (Group, error) {
	var g Group
	_, err := c.getJSON(ctx, c.endpoint("groups/"+url.PathEscape(path), nil), &g)
	return g, err
}

// GroupProjects lists the projects in a group and its subgroups, without
// projects shared into it from elsewhere.
func (c *Client) GroupProjects(ctx context.Context, groupID int64, maxPages int) ([]Project, bool, error) {
	q := url.Values{"include_subgroups": {"true"}, "with_shared": {"false"}, "order_by": {"id"}, "sort": {"asc"}, "per_page": {"100"}}
	var out []Project
	complete, err := c.pages(ctx, c.endpoint(fmt.Sprintf("groups/%d/projects", groupID), q), maxPages, func(body []byte) error {
		var page []Project
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		out = append(out, page...)
		return nil
	})
	return out, complete, err
}

// Tree lists the entries directly under dir ("" for the root) at ref. It
// reports false when there were more than maxPages pages.
func (c *Client) Tree(ctx context.Context, projectID int64, ref, dir string, maxPages int) ([]TreeEntry, bool, error) {
	q := url.Values{"ref": {ref}, "per_page": {"100"}}
	if dir != "" {
		q.Set("path", dir)
	}
	var out []TreeEntry
	complete, err := c.pages(ctx, c.endpoint(fmt.Sprintf("projects/%d/repository/tree", projectID), q), maxPages, func(body []byte) error {
		var page []TreeEntry
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		out = append(out, page...)
		return nil
	})
	return out, complete, err
}

// RawFile returns at most limit bytes of the file at path, at ref.
func (c *Client) RawFile(ctx context.Context, projectID int64, ref, path string, limit int64) ([]byte, error) {
	u := c.endpoint(fmt.Sprintf("projects/%d/repository/files/%s/raw", projectID, url.PathEscape(path)), url.Values{"ref": {ref}})
	// The path is in the URL, so errors name the endpoint without it.
	resp, err := c.do(ctx, u, fmt.Sprintf("/projects/%d/repository/files/(path)/raw", projectID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Branches returns up to max branch names.
func (c *Client) Branches(ctx context.Context, projectID int64, max int) ([]string, error) {
	var out []string
	_, err := c.pages(ctx, c.endpoint(fmt.Sprintf("projects/%d/repository/branches", projectID), url.Values{"per_page": {"100"}}),
		(max+99)/100, func(body []byte) error {
			var page []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(body, &page); err != nil {
				return err
			}
			for _, b := range page {
				if len(out) < max {
					out = append(out, b.Name)
				}
			}
			return nil
		})
	return out, err
}

// getJSON decodes one response from u into v.
func (c *Client) getJSON(ctx context.Context, u *url.URL, v any) (http.Header, error) {
	resp, err := c.do(ctx, u, "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(v); err != nil {
		return nil, fmt.Errorf("GitLab GET %s: decoding the response: %w", c.display(u), err)
	}
	return resp.Header, nil
}

// endpoint returns the API URL for escPath, a path already escaped (a group
// path is one segment, its slashes as %2F), with query q.
func (c *Client) endpoint(escPath string, q url.Values) *url.URL {
	u := *c.api
	u.RawPath = c.api.EscapedPath() + escPath
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.RawQuery = q.Encode()
	return &u
}

// pages calls each page of a listing in turn, following GitLab's Link header
// (or X-Next-Page), and reports false when there were more than maxPages. A
// next link to anywhere but this API is an error: the token is never sent
// elsewhere.
func (c *Client) pages(ctx context.Context, u *url.URL, maxPages int, each func([]byte) error) (bool, error) {
	for range maxPages {
		resp, err := c.do(ctx, u, "")
		if err != nil {
			return false, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes))
		_ = resp.Body.Close()
		if err != nil {
			return false, err
		}
		if err := each(body); err != nil {
			return false, fmt.Errorf("GitLab GET %s: decoding the response: %w", c.display(u), err)
		}
		next, err := c.next(u, resp.Header)
		if err != nil || next == nil {
			return err == nil, err
		}
		u = next
	}
	return false, nil
}

func (c *Client) next(cur *url.URL, h http.Header) (*url.URL, error) {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		target, params, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(params, `rel="next"`) {
			continue
		}
		u, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			return nil, fmt.Errorf("GitLab GET %s: unreadable next-page link", c.display(cur))
		}
		if u.Scheme != c.api.Scheme || u.Host != c.api.Host || !strings.HasPrefix(u.Path, c.api.Path) {
			return nil, fmt.Errorf("GitLab GET %s: next-page link points outside the API; not followed", c.display(cur))
		}
		return u, nil
	}
	if p := strings.TrimSpace(h.Get("X-Next-Page")); p != "" {
		if _, err := strconv.Atoi(p); err != nil {
			return nil, fmt.Errorf("GitLab GET %s: unreadable next page", c.display(cur))
		}
		u := *cur
		q := u.Query()
		q.Set("page", p)
		u.RawQuery = q.Encode()
		return &u, nil
	}
	return nil, nil
}

// display is the request path relative to the API, for error text.
func (c *Client) display(u *url.URL) string {
	return "/" + strings.TrimPrefix(u.EscapedPath(), c.api.EscapedPath())
}

// do sends a GET and returns the response when it succeeded. shown, when set,
// replaces the request path in an error.
func (c *Client) do(ctx context.Context, u *url.URL, shown string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// The transport error quotes the URL; keep only what went wrong.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		if shown == "" {
			shown = c.display(u)
		}
		return nil, fmt.Errorf("GitLab GET %s: %w", shown, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if shown == "" {
		shown = c.display(u)
	}
	e := &Error{Method: http.MethodGet, Path: shown, Status: resp.StatusCode, Message: message(resp.Body)}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Err: e, Reset: reset(resp.Header)}
	}
	return nil, e
}

// message returns the server's error message on one line, bounded.
func message(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 64<<10))
	var v struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	text := ""
	if json.Unmarshal(raw, &v) == nil {
		switch m := v.Message.(type) {
		case string:
			text = m
		case nil:
			text = v.Error
		default:
			b, _ := json.Marshal(m)
			text = string(b)
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxMessageBytes {
		text = strings.ToValidUTF8(text[:maxMessageBytes], "") + "…"
	}
	return text
}

// reset reads when a rate limit lifts: RateLimit-Reset (a Unix time), else
// Retry-After (seconds). Zero when neither says.
func reset(h http.Header) time.Time {
	if s, err := strconv.ParseInt(strings.TrimSpace(h.Get("RateLimit-Reset")), 10, 64); err == nil && s > 0 {
		return time.Unix(s, 0)
	}
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s >= 0 {
		return time.Now().Add(time.Duration(s) * time.Second)
	}
	return time.Time{}
}
