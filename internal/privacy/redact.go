package privacy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
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
// It matches each full slug it is given, ignoring case, since forge names are
// case-insensitive and a log line can spell one differently from the slug
// (API URLs use GitHub's spelling of the owner, the slug the config's). A slug
// is "owner/name" on GitHub and can nest deeper, "group/sub/name", on GitLab;
// URL-encoded spellings are passed in as further slugs. It does not know about
// bare repo names. A nil *Redactor is a no-op.
//
// In exclude mode its slog handler drops a record that mentions a protected
// repo instead of masking it, since that mode promises the repo is absent from
// the output, and a masked line still shows that it exists.
type Redactor struct {
	re   *regexp.Regexp
	drop bool
	// public holds the scan's public slugs. A match inside a whole one of
	// them is left alone; any other match is redacted.
	public []string
	// protected holds the protected slugs, to check that none overlaps the
	// edge of a public name a match would be left in.
	protected []string
}

// NewRedactor returns a Redactor for the protected repos in vis, or nil when
// the guard is inactive (not a public context, or mode allow) or there is
// nothing to protect.
func NewRedactor(vis map[string]string, o Options) *Redactor {
	if !o.PublicContext || o.Mode == ModeAllow || o.Mode == "" {
		return nil
	}
	var slugs []string
	publicSet := map[string]bool{}
	for slug, v := range vis {
		if protected(v) {
			slugs = append(slugs, slug)
		} else {
			publicSet[strings.ToLower(slug)] = true
		}
	}
	if len(slugs) == 0 {
		return nil
	}
	// A spelling that is both protected and public fails closed.
	for _, p := range slugs {
		delete(publicSet, strings.ToLower(p))
	}
	public := make([]string, 0, len(publicSet))
	for p := range publicSet {
		public = append(public, p)
	}
	// Longest first: an alternation prefers its earlier branches, so a shorter
	// slug must not match the prefix of a longer one ("o/app" inside "o/app-x")
	// and leave the rest of the name behind.
	sort.Slice(slugs, func(i, j int) bool { return len(slugs[i]) > len(slugs[j]) })
	protectedSlugs := slices.Clone(slugs)
	for i, s := range slugs {
		slugs[i] = regexp.QuoteMeta(s)
	}
	return &Redactor{re: regexp.MustCompile("(?i)" + strings.Join(slugs, "|")), drop: o.Mode == ModeExclude, public: public, protected: protectedSlugs}
}

// String returns s with every protected slug replaced. A match is left alone
// only when it lies inside a whole public name, one of the scan's public
// slugs ("acme/open" inside a public "acme/open-kit", "bigacme/open" or
// "acme/open/cli"), since masking part of a public name would show what the
// private one is. Anything else is redacted: a name the redactor does not know
// is over-redacted, never left readable. Matches can overlap ("x/y/g" and
// "g/app" in "x/y/g/app"), so every one is found and their union is masked;
// masking only the first would leave the rest of the second readable.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	type span struct{ start, end int }
	var masked []span
	for from := 0; from < len(s); {
		loc := r.re.FindStringIndex(s[from:])
		if loc == nil {
			break
		}
		start, end := from+loc[0], from+loc[1]
		if !r.spared(s, start, end) {
			if n := len(masked); n > 0 && start < masked[n-1].end {
				masked[n-1].end = max(masked[n-1].end, end)
			} else {
				masked = append(masked, span{start, end})
			}
		}
		from = start + 1
	}
	if len(masked) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range masked {
		b.WriteString(s[last:m.start])
		b.WriteString(RedactedSlug)
		last = m.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// spared reports whether the match s[start:end] is left alone: a public slug
// occurs around it as a whole name, bounded by characters that cannot be part
// of a name (a trailing dot or ".git" ends it), and no protected slug crosses
// either edge of that name. A protected slug that did would be redacted and
// cut into the public name, leaving the spared match readable on its own.
func (r *Redactor) spared(s string, start, end int) bool {
	_, ns, _ := enclosingName(s, start, end)
	for _, p := range r.public {
		for ps := max(0, end-len(p)); ps <= ns && ps+len(p) <= len(s); ps++ {
			pe := ps + len(p)
			if !strings.EqualFold(s[ps:pe], p) || (ps > 0 && isNameByte(s[ps-1])) {
				continue
			}
			tail := pe
			for tail < len(s) && isNameByte(s[tail]) {
				tail++
			}
			if rest := strings.TrimRight(s[pe:tail], "."); rest != "" && !strings.EqualFold(rest, ".git") {
				continue
			}
			if !r.crosses(s, ps) && !r.crosses(s, pe) && !r.crosses(s, tail) {
				return true
			}
		}
	}
	return false
}

// enclosingName returns the name that s[start:end] sits in, and where it
// starts and ends in s: the match extended over the name characters on
// either side. Trailing dots and a trailing ".git", which end a sentence or a
// clone URL rather than the name, are left out of the returned name.
func enclosingName(s string, start, end int) (string, int, int) {
	for start > 0 && isNameByte(s[start-1]) {
		start--
	}
	for end < len(s) && isNameByte(s[end]) {
		end++
	}
	name := strings.TrimRight(s[start:end], ".")
	if len(name) > 4 && strings.EqualFold(name[len(name)-4:], ".git") {
		name = name[:len(name)-4]
	}
	return name, start, end
}

// crosses reports whether a protected slug occurs across the boundary before
// s[b], with characters on both sides of it: the only way one can cut into a
// name that starts or ends there. (?i) matching also folds a few non-ASCII
// letters, such as the Kelvin sign, which this byte comparison does not; such
// a spelling is not something a forge or baseliner emits.
func (r *Redactor) crosses(s string, b int) bool {
	for _, p := range r.protected {
		for st := max(0, b-len(p)+1); st < b && st+len(p) <= len(s); st++ {
			if st+len(p) > b && strings.EqualFold(s[st:st+len(p)], p) {
				return true
			}
		}
	}
	return false
}

// isNameByte reports whether c can appear in a forge owner, group or repo name.
func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
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
