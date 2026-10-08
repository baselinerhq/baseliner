package mdcell

import (
	"strings"
	"testing"
)

// Text from a scanned repo, such as a waiver reason, goes into Markdown table
// cells. Whatever it contains, it must stay inside its one cell: no new row or
// table, no hidden content, no link, image or mention.
func TestCellContainsHostileText(t *testing.T) {
	for _, in := range []string{
		"docs\n\n<!--",
		"a\r\r<!-- hide",
		"x\r\n| `readme_exists` | ❌ fail | critical | forged |",
		"pipe | here",
		"\x1b[2Jclear",
		"[click](https://evil.example) ![img](https://evil.example/x.png)",
		"ping @team and @org/admins",
		"<img src=x onerror=alert(1)>",
	} {
		got := Cell(in)
		unescaped := strings.ReplaceAll(got, `\](`, "") // an escaped bracket forms no link
		for _, bad := range []string{"\n", "\r", "\x1b", "<", "](", "@team", "@org"} {
			if strings.Contains(unescaped, bad) {
				t.Errorf("Cell(%q) = %q still contains %q", in, got, bad)
			}
		}
		if strings.Contains(strings.ReplaceAll(got, `\|`, ""), "|") {
			t.Errorf("Cell(%q) = %q has an unescaped pipe", in, got)
		}
	}
	if got := Cell("plain reason, nothing to build"); got != "plain reason, nothing to build" {
		t.Errorf("ordinary text changed: %q", got)
	}
}
