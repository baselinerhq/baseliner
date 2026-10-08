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
type Redactor struct {
	re *regexp.Regexp
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
	return &Redactor{re: regexp.MustCompile("(?i)" + strings.Join(slugs, "|"))}
}

// String returns s with every protected slug replaced.
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	return r.re.ReplaceAllLiteralString(s, RedactedSlug)
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
}

func (rh redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return rh.h.Enabled(ctx, l)
}

func (rh redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, rh.r.String(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(rh.attr(a))
		return true
	})
	return rh.h.Handle(ctx, out)
}

func (rh redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(as))
	for i, a := range as {
		red[i] = rh.attr(a)
	}
	return redactHandler{h: rh.h.WithAttrs(red), r: rh.r}
}

func (rh redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{h: rh.h.WithGroup(name), r: rh.r}
}

// attr redacts a string, group, or arbitrary value (an error, a panic value);
// numbers, bools, times and durations cannot carry a slug and pass through.
func (rh redactHandler) attr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, rh.r.String(v.String()))
	case slog.KindGroup:
		g := v.Group()
		red := make([]any, len(g))
		for i, ga := range g {
			red[i] = rh.attr(ga)
		}
		return slog.Group(a.Key, red...)
	case slog.KindAny:
		return slog.String(a.Key, rh.r.String(fmt.Sprint(v.Any())))
	default:
		return slog.Attr{Key: a.Key, Value: v}
	}
}
