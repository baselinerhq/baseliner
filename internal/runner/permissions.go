package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxErrorBody bounds the error body read to add a permission hint to.
const maxErrorBody = 64 << 10

// permissionHints names the missing permission in GitHub's refusals. A
// fine-grained token or GitHub App token refused for want of a permission
// gets a 403 whose X-Accepted-GitHub-Permissions header lists what the
// endpoint accepts. Its JSON message only says the resource is not
// accessible. This adds the header's sets to that message, so every error
// built from it, wherever it is logged or reported, says what to grant.
type permissionHints struct {
	base http.RoundTripper
}

func (t permissionHints) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		return resp, err
	}
	accepted := strings.TrimSpace(resp.Header.Get("X-Accepted-GitHub-Permissions"))
	if accepted == "" {
		return resp, nil
	}
	orig := resp.Body
	body, rerr := io.ReadAll(io.LimitReader(orig, maxErrorBody+1))
	if rerr != nil || len(body) > maxErrorBody {
		// Pass it on as received: what was read, then the rest.
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), orig), orig}
		return resp, nil
	}
	_ = orig.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var msg map[string]any
	if json.Unmarshal(body, &msg) != nil {
		return resp, nil
	}
	m, _ := msg["message"].(string)
	msg["message"] = strings.TrimSpace(m + " (the token needs " + describePermissions(accepted) + ")")
	out, merr := json.Marshal(msg)
	if merr != nil {
		return resp, nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
	return resp, nil
}

// describePermissions turns the header's value, comma-separated permissions
// in semicolon-separated alternative sets, into prose:
// "contents=read" -> "contents: read";
// "pull_requests=read,contents=read; issues=read,contents=read" ->
// "pull_requests: read and contents: read, or issues: read and contents: read".
func describePermissions(accepted string) string {
	var sets []string
	for _, set := range strings.Split(accepted, ";") {
		var perms []string
		for _, p := range strings.Split(set, ",") {
			if p = strings.TrimSpace(p); p != "" {
				perms = append(perms, strings.Replace(p, "=", ": ", 1))
			}
		}
		if len(perms) > 0 {
			sets = append(sets, strings.Join(perms, " and "))
		}
	}
	return strings.Join(sets, ", or ")
}
