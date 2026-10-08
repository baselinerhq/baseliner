// Package mdcell makes text safe to place in one cell of a Markdown table. Cell
// is for text that may come from a scanned repo, such as a waiver reason; Code
// is for a slug shown in a code span; Trusted is for text from the central
// config, such as policy_info.
package mdcell

import (
	"strings"
	"unicode"
)

// Cell returns s as one code span for a table cell. Text from a scanned repo,
// such as a waiver reason or a branch name, then renders as the text it is:
// GitHub forms no HTML, link, autolink, issue or commit reference, mention,
// emoji or math inside a code span, and escaping each of those forms
// separately does not hold, because GitHub's own reference and emoji filters
// run after Markdown escapes are gone. Line breaks of every kind become
// spaces, control and invisible format characters (bidi overrides,
// zero-width marks) are dropped, and pipes are escaped so the cell cannot be
// split. The fence is one backtick longer than the longest run in the text.
func Cell(s string) string {
	var b strings.Builder
	run, longest := 0, 0
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			continue
		case r == '|':
			b.WriteString(`\|`)
		default:
			b.WriteRune(r)
		}
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return ""
	}
	fence := strings.Repeat("`", longest+1)
	// The spaces keep a backtick at either end from joining the fence; one is
	// stripped from each side when rendered.
	return fence + " " + text + " " + fence
}

// Code returns s for a code span in a table cell: a code span shows its text
// literally, so only pipes, which would split the cell, are escaped, line
// breaks become spaces, and control characters are dropped.
func Code(s string) string {
	return tableSafe(s)
}

// Trusted returns text from the central config for a table cell: its Markdown
// is kept, so links still form, and only pipes and line breaks, which would
// break the table, are changed.
func Trusted(s string) string {
	return tableSafe(s)
}

func tableSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		case r == '|':
			b.WriteString(`\|`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
