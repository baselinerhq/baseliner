package mdcell

import (
	"strings"
	"testing"
)

// Text from a scanned repo, such as a waiver reason, goes into Markdown table
// cells. Whatever it contains, it must be exactly one code span on one line,
// with no pipe left to split the cell: GitHub renders a code span's text as
// text, where escaping each Markdown form does not stop its emoji, issue and
// commit reference filters (each case below rendered through GitHub's Markdown
// API: plain text in a code span).
func TestCellIsOneCodeSpan(t *testing.T) {
	for _, in := range []string{
		"docs\n\n<!--",
		"a\r\r<!-- hide",
		"x\r\n| `readme_exists` | ❌ fail | critical | forged |",
		"pipe | here and a\\|b",
		"\x1b[2Jclear",
		"[click](https://evil.example) ![img](https://evil.example/x.png)",
		"see https://evil.example/login or www.evil.example or a@b.co",
		"fixes #1, GH-147, owner/repo#147 and 2638db4fc824ed065219fc146d3554186d5b96ea",
		"ping @team and @org/admins",
		"<img src=x onerror=alert(1)> &lt;!-- &#124;",
		"$\\rule{50em}{20em}$ and $$\\color{red}{FAIL}$$",
		"**bold** __bold__ ~~gone~~ :shipit: :smile:",
		"`starts and ends with a backtick`",
		"``double`` and ```triple```",
		"abc\u202edcba\u2066x\u2069 line\u2028sep\u2029para\u0085nel",
	} {
		got := Cell(in)
		fence := got[:strings.IndexFunc(got, func(r rune) bool { return r != '`' })]
		inner := strings.TrimSuffix(strings.TrimPrefix(got, fence+" "), " "+fence)
		if fence == "" || !strings.HasSuffix(got, " "+fence) || inner == got {
			t.Errorf("Cell(%q) = %q is not one code span", in, got)
			continue
		}
		if strings.Contains(inner, fence) {
			t.Errorf("Cell(%q) = %q: the text contains its own fence", in, got)
		}
		for _, bad := range []string{"\n", "\r", "\x1b", "\u202e", "\u2066", "\u2028", "\u2029", "\u0085"} {
			if strings.Contains(got, bad) {
				t.Errorf("Cell(%q) = %q still contains %q", in, got, bad)
			}
		}
		if strings.Contains(strings.ReplaceAll(got, `\|`, ""), "|") {
			t.Errorf("Cell(%q) = %q has an unescaped pipe", in, got)
		}
	}
	if got := Cell("plain reason"); got != "` plain reason `" {
		t.Errorf("Cell(plain) = %q", got)
	}
	if got := Cell(" \n\u200b "); got != "" {
		t.Errorf("Cell(blank) = %q, want empty", got)
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
