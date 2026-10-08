package privacy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// RedactedSlug replaces a protected repo's slug in free text (log lines and
// other stderr output).
const RedactedSlug = "private/redacted"

// Redactor masks protected repo slugs in text bound for stderr. Apply covers
// the structured output (table, JSON, SARIF, Markdown); a Redactor covers
// everything else that reaches the log — slog records and the runner's own
// messages — which would otherwise print the real slug in a public context.
//
// It matches each full "owner/name" it is given, ignoring case, since GitHub
// owner and repo names are case-insensitive and a log line can spell one
// differently from the slug (API URLs use GitHub's spelling of the owner, the
// slug the config's). It does not know about bare repo names. A nil *Redactor
// is a no-op.
//
// In exclude mode its slog handler drops a record that mentions a protected
// repo instead of masking it, since that mode promises the repo is absent from
// the output, and a masked line still shows that it exists.
type Redactor struct {
	re   *regexp.Regexp
	drop bool
}

// NewRedactor returns a Redactor for the protected repos in vis, or nil when
// the guard is inactive (not a public context, or mode allow) or there is
// nothing to protect.
func NewRedactor(vis map[string]string, o Options) *Redactor {
	if !o.PublicContext || o.Mode == ModeAllow || o.Mode == "" {
		return nil
	}
	var slugs []string
	for slug, v := range vis {
		if protected(v) {
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return nil
	}
	// Longest first: an alternation prefers its earlier branches, so a shorter
	// slug must not match the prefix of a longer one ("o/app" inside "o/app-x")
	// and leave the rest of the name behind.
	sort.Slice(slugs, func(i, j int) bool { return len(slugs[i]) > len(slugs[j]) })
	for i, s := range slugs {
		slugs[i] = regexp.QuoteMeta(s)
	}
	return &Redactor{re: regexp.MustCompile("(?i)" + strings.Join(slugs, "|")), drop: o.Mode == ModeExclude}
}

// String returns s with every protected slug replaced. A slug is replaced
// only as a whole name: not where it is the start or end of a longer one
// ("acme/open" in "acme/open-kit" or "bigacme/open"), since masking part of a
// public name would show what the private one is.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for from := 0; from < len(s); {
		loc := r.re.FindStringIndex(s[from:])
		if loc == nil {
			break
		}
		start, end := from+loc[0], from+loc[1]
		if start > 0 && isNameByte(s[start-1]) || continuesName(s[end:]) {
			from = start + 1
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(RedactedSlug)
		last, from = end, end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// isNameByte reports whether c can appear in a GitHub owner or repo name.
func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
}

// continuesName reports whether rest, the text after a match, carries the
// name on. A '.' does so only when a name character follows it, so a slug at
// the end of a sentence is still a whole name; ".git" does not, so a clone URL
// still is.
func continuesName(rest string) bool {
	if rest == "" || !isNameByte(rest[0]) {
		return false
	}
	if rest[0] != '.' {
		return true
	}
	if len(rest) >= 4 && strings.EqualFold(rest[:4], ".git") && (len(rest) == 4 || !isNameByte(rest[4])) {
		return false
	}
	return len(rest) > 1 && isNameByte(rest[1])
}

// Writer wraps w so every write is redacted. Each Write is redacted on its own,
// so a slug split across two writes is not caught; slog handlers and fmt.Fprint
// each write a whole line at a time.
func (r *Redactor) Writer(w io.Writer) io.Writer {
	if r == nil {
		return w
	}
	return redactWriter{w: w, r: r}
}

type redactWriter struct {
	w io.Writer
	r *Redactor
}

func (rw redactWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(rw.w, rw.r.String(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Handler wraps h so each record's message and attribute values are redacted
// before h sees them.
func (r *Redactor) Handler(h slog.Handler) slog.Handler {
	if r == nil {
		return h
	}
	return redactHandler{h: h, r: r}
}

type redactHandler struct {
	h slog.Handler
	r *Redactor
	// mentions records that an attribute bound with WithAttrs names a
	// protected repo, so in exclude mode every record from it is dropped.
	mentions bool
}

func (rh redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return rh.h.Enabled(ctx, l)
}

func (rh redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	msg := rh.r.String(rec.Message)
	mentions := rh.mentions || msg != rec.Message
	out := slog.NewRecord(rec.Time, rec.Level, msg, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		red, m := rh.attr(a)
		mentions = mentions || m
		out.AddAttrs(red)
		return true
	})
	if mentions && rh.r.drop {
		return nil
	}
	return rh.h.Handle(ctx, out)
}

func (rh redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(as))
	mentions := rh.mentions
	for i, a := range as {
		var m bool
		red[i], m = rh.attr(a)
		mentions = mentions || m
	}
	return redactHandler{h: rh.h.WithAttrs(red), r: rh.r, mentions: mentions}
}

func (rh redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{h: rh.h.WithGroup(name), r: rh.r, mentions: rh.mentions}
}

// attr redacts a string, group, or arbitrary value (an error, a panic value),
// and reports whether it named a protected repo; numbers, bools, times and
// durations cannot carry a slug and pass through.
func (rh redactHandler) attr(a slog.Attr) (slog.Attr, bool) {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		red := rh.r.String(v.String())
		return slog.String(a.Key, red), red != v.String()
	case slog.KindGroup:
		g := v.Group()
		red := make([]any, len(g))
		mentions := false
		for i, ga := range g {
			ra, m := rh.attr(ga)
			red[i] = ra
			mentions = mentions || m
		}
		return slog.Group(a.Key, red...), mentions
	case slog.KindAny:
		s := fmt.Sprint(v.Any())
		red := rh.r.String(s)
		return slog.String(a.Key, red), red != s
	default:
		return slog.Attr{Key: a.Key, Value: v}, false
	}
}
