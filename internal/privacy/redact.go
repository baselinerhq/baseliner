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
	// public holds the scan's public slugs, lowercased. A match inside one of
	// them is left alone; any other match is redacted.
	public map[string]bool
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
	public := map[string]bool{}
	for slug, v := range vis {
		if protected(v) {
			slugs = append(slugs, slug)
		} else {
			public[strings.ToLower(slug)] = true
		}
	}
	if len(slugs) == 0 {
		return nil
	}
	// A spelling that is both protected and public fails closed.
	for _, p := range slugs {
		delete(public, strings.ToLower(p))
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
// only when the whole name around it is one of the scan's public slugs
// ("acme/open" inside a public "acme/open-kit" or "bigacme/open"), since
// masking part of a public name would show what the private one is. Anything
// else is redacted: a name the redactor does not know is over-redacted, never
// left readable.
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
		if r.spared(s, start, end) {
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

// spared reports whether the match s[start:end] is left alone: the name it
// sits in is one of the scan's public slugs, and no protected slug crosses that
// name's edge. A protected slug that did would be redacted and cut into the
// public name, leaving the spared match readable on its own.
func (r *Redactor) spared(s string, start, end int) bool {
	name, ns, ne := enclosingName(s, start, end)
	return r.public[strings.ToLower(name)] && !r.crossesAt(s, ns-1) && !r.crossesAt(s, ne)
}

// enclosingName returns the owner/name that s[start:end] sits in, and where
// it starts and ends in s: the match extended over the name characters on
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

// crossesAt reports whether a protected slug has its "/" at s[i], which is the
// only way one can overlap a name that stops at i. (?i) matching also folds a
// few non-ASCII letters, such as the Kelvin sign, which this byte comparison
// does not; such a spelling is not something GitHub or baseliner emits.
func (r *Redactor) crossesAt(s string, i int) bool {
	if i < 0 || i >= len(s) || s[i] != '/' {
		return false
	}
	for _, p := range r.protected {
		k := strings.IndexByte(p, '/')
		if st := i - k; st >= 0 && st+len(p) <= len(s) && strings.EqualFold(s[st:st+len(p)], p) {
			return true
		}
	}
	return false
}

// isNameByte reports whether c can appear in a GitHub owner or repo name.
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
