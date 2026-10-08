// Package mdcell makes text safe to place in one cell of a Markdown table. Cell
// is for text that may come from a scanned repo, such as a waiver reason; Code
// is for a slug shown in a code span; Trusted is for text from the central
// config, such as policy_info.
package mdcell

import (
	"strings"
	"unicode"
)

// active is the ASCII punctuation Markdown or GitHub can give meaning to. A
// backslash before any of them renders the character itself.
const active = "\\`*_{}[]()+-.!|>:~&="

// Cell returns s so that it stays in its one table cell and renders as the
// text it is. Line breaks of every kind become spaces; control and format
// characters (bidi overrides, zero-width marks) are dropped; Markdown-active
// punctuation is backslash-escaped, so no emphasis, link, image, autolink,
// entity or emoji forms; "<" is an entity, so no HTML; "$" is a full-width
// dollar, so no math; and a zero-width space after "#" and "@" stops issue
// references and mentions.
func Cell(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n' || r == '\u0085' || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		case r == '<':
			b.WriteString("&lt;")
		case r == '$':
			b.WriteRune('\uff04') // full-width dollar sign
		case r == '#' || r == '@':
			b.WriteRune(r)
			b.WriteRune('\u200b') // a zero-width space stops the reference
		case r < 0x80 && strings.ContainsRune(active, r):
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
