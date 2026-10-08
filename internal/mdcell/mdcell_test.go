package mdcell

import (
	"strings"
	"testing"
)

// Text from a scanned repo, such as a waiver reason, goes into Markdown table
// cells. Whatever it contains, it must stay inside its one cell and render as
// the text it is: no new row, no hidden content, no HTML, link, autolink,
// issue reference, mention, math, emphasis or emoji, and no reordering.
func TestCellContainsHostileText(t *testing.T) {
	for _, in := range []string{
		"docs\n\n<!--",
		"a\r\r<!-- hide",
		"x\r\n| `readme_exists` | ❌ fail | critical | forged |",
		"pipe | here and a\\|b",
		"\x1b[2Jclear",
		"[click](https://evil.example) ![img](https://evil.example/x.png)",
		"see https://evil.example/login or www.evil.example",
		"fixes #1 and owner/repo#147",
		"ping @team and @org/admins",
		"<img src=x onerror=alert(1)> &lt;!-- &#124;",
		"$\\rule{50em}{20em}$ and $$\\color{red}{FAIL}$$",
		"**bold** __bold__ ~~gone~~ `code` :shipit:",
		"abc\u202edcba\u2066x\u2069 line\u2028sep\u2029para\u0085nel",
	} {
		got := Cell(in)
		bare := stripEscapes(got) // what is left once escaped characters are taken out
		for _, bad := range []string{"\n", "\r", "\x1b", "<", "://", "www.", "$", "**", "__", "~~", "`", ":shipit:", "\u202e", "\u2066", "\u2028", "\u2029", "\u0085", "&#", "&lt;!"} {
			if strings.Contains(bare, bad) {
				t.Errorf("Cell(%q) = %q still contains %q", in, got, bad)
			}
		}
		for _, ref := range []string{"#1", "#147", "@team", "@org"} {
			if strings.Contains(got, ref) {
				t.Errorf("Cell(%q) = %q still contains the reference %q", in, got, ref)
			}
		}
		// Every pipe is escaped, so the cell cannot be split.
		if strings.Contains(bare, "|") {
			t.Errorf("Cell(%q) = %q has an unescaped pipe", in, got)
		}
	}
	if got := Cell("plain reason nothing to build"); got != "plain reason nothing to build" {
		t.Errorf("ordinary text changed: %q", got)
	}
}

// A slug in a code span shows literally, so only what would break the table
// is touched.
func TestCodeKeepsPaths(t *testing.T) {
	if got := Code("/tmp/my[repo]!@x"); got != "/tmp/my[repo]!@x" {
		t.Errorf("Code changed a path: %q", got)
	}
	if got := Code("a|b\nc\x1b[2J"); got != `a\|b c[2J` {
		t.Errorf("Code(%q) = %q", "a|b\nc\x1b[2J", got)
	}
}

// Text from the central config is trusted: its links still form, and only
// what would break the table is touched.
func TestTrustedKeepsLinks(t *testing.T) {
	in := "see [the standard](https://example.com/std) | now"
	if got := Trusted(in); got != `see [the standard](https://example.com/std) \| now` {
		t.Errorf("Trusted(%q) = %q", in, got)
	}
}

// stripEscapes removes each backslash-escaped character, leaving only text
// Markdown would interpret.
func stripEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
