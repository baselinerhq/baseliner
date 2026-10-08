// Package mdcell makes text safe to place in one cell of a Markdown table,
// for text that may come from a scanned repo, such as a waiver reason.
package mdcell

import (
	"strings"
	"unicode"
)

// Cell returns s with everything that could leave its table cell, or change
// how the document renders, neutralised: line breaks become spaces, other
// control characters are dropped, pipes are escaped, HTML is not parsed,
// links and images do not form, and @-mentions do not ping.
func Cell(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		case r == '|':
			b.WriteString(`\|`)
		case r == '<':
			b.WriteString("&lt;")
		case r == '[' || r == ']' || r == '!':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '@':
			b.WriteString("@\u200b") // a zero-width space stops the mention
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
