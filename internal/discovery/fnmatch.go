package discovery

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// globMatch reports whether name matches a shell-style pattern using the same
// semantics as Python's fnmatch (the implementation the original baseliner used),
// rather than Go's path.Match. The two differ in ways that silently change which
// repos are selected:
//   - negated character classes use `[!seq]`, not `[^seq]` (a bare `^` is literal);
//   - a malformed pattern (e.g. an unterminated `[`) is treated literally and can
//     still match, instead of failing to a non-match.
//
// Patterns are translated to anchored regexps once and cached.
func globMatch(pattern, name string) bool {
	re := compileGlob(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(name)
}

var globCache sync.Map // pattern string -> *regexp.Regexp (nil on compile failure)

func compileGlob(pattern string) *regexp.Regexp {
	if v, ok := globCache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	re, err := regexp.Compile(translateGlob(pattern))
	if err != nil {
		// translateGlob always emits a valid expression, so this is unexpected.
		slog.Warn("invalid glob pattern, ignoring", "pattern", pattern, "err", err)
		re = nil
	}
	globCache.Store(pattern, re)
	return re
}

// translateGlob converts a Python fnmatch pattern into an anchored Go regexp
// string, following CPython's fnmatch.translate semantics.
func translateGlob(pat string) string {
	var b strings.Builder
	b.WriteString(`(?s)\A`)
	i, n := 0, len(pat)
	for i < n {
		c := pat[i]
		i++
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		case '[':
			j := i
			if j < n && pat[j] == '!' {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				// Unterminated class: treat '[' as a literal (fnmatch behavior).
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			stuff := strings.ReplaceAll(pat[i:j], `\`, `\\`)
			i = j + 1
			switch {
			case stuff == "":
				b.WriteString(`(?!)`) // empty class never matches
			case stuff == "!":
				b.WriteByte('.') // [!] is the negation of an empty set: any char
			default:
				if stuff[0] == '!' {
					stuff = "^" + stuff[1:]
				} else if stuff[0] == '^' || stuff[0] == '[' {
					stuff = `\` + stuff
				}
				b.WriteByte('[')
				b.WriteString(stuff)
				b.WriteByte(']')
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`\z`)
	return b.String()
}
