package checks

import (
	"strings"
	"unicode"

	"github.com/baselinerhq/baseliner/internal/models"
)

type readmeExists struct{ base }

func (c readmeExists) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.KeyFiles["README"] {
		return c.pass()
	}
	return c.fail("No README file found")
}

type readmeNonEmpty struct{ base }

func (c readmeNonEmpty) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.ReadmeContent == nil {
		return c.fail("README not found")
	}
	if len(strings.TrimSpace(*r.FS.ReadmeContent)) > 0 {
		return c.pass()
	}
	return c.fail("README is present but empty")
}

type readmeHasHeading struct{ base }

func (c readmeHasHeading) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.ReadmeContent == nil {
		return c.fail("README not found")
	}
	lines := splitLines(*r.FS.ReadmeContent)
	if hasMarkdownHeading(lines) || hasUnderlineHeading(lines) {
		return c.pass()
	}
	return c.fail("README has no headings (expected at least one # heading or underline heading)")
}

// splitLines splits on the same boundaries as Python's str.splitlines() (all
// Unicode line breaks, dropping a trailing empty element) so heading detection
// behaves identically regardless of line endings.
func splitLines(s string) []string {
	var lines []string
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, b.String())
			b.Reset()
			if r == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++ // treat \r\n as a single break
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}

func hasMarkdownHeading(lines []string) bool {
	for _, line := range lines {
		// strings.TrimLeftFunc with unicode.IsSpace mirrors Python str.lstrip(),
		// which strips all Unicode whitespace (not just space and tab).
		if strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), "#") {
			return true
		}
	}
	return false
}

func hasUnderlineHeading(lines []string) bool {
	for i := 0; i < len(lines)-1; i++ {
		title := strings.TrimSpace(lines[i])
		underline := strings.TrimSpace(lines[i+1])
		if title == "" || underline == "" || len(underline) < 3 {
			continue
		}
		if strings.Trim(underline, "=") == "" || strings.Trim(underline, "-") == "" {
			return true
		}
	}
	return false
}

type licenseExists struct{ base }

func (c licenseExists) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.KeyFiles["LICENSE"] {
		return c.pass()
	}
	return c.fail("No LICENSE or COPYING file found")
}

type gitignoreExists struct{ base }

func (c gitignoreExists) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.KeyFiles["GITIGNORE"] {
		return c.pass()
	}
	return c.fail("No .gitignore found")
}

type ciPresent struct{ base }

func (c ciPresent) Eval(r *models.NormalizedRepository) models.CheckResult {
	if len(r.FS.CIFiles) > 0 {
		return c.pass()
	}
	return c.fail("No CI workflow files found")
}

type codeownersExists struct{ base }

func (c codeownersExists) Eval(r *models.NormalizedRepository) models.CheckResult {
	if r.FS.KeyFiles["CODEOWNERS"] {
		return c.pass()
	}
	return c.fail("No CODEOWNERS file found")
}

type dependencyUpdateConfig struct{ base }

func (c dependencyUpdateConfig) Eval(r *models.NormalizedRepository) models.CheckResult {
	if len(r.FS.DepUpdateFiles) > 0 {
		return c.pass()
	}
	return c.fail("No Dependabot or Renovate config found")
}
