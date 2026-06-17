package checks

import (
	"strings"

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
	lines := strings.Split(*r.FS.ReadmeContent, "\n")
	if hasMarkdownHeading(lines) || hasUnderlineHeading(lines) {
		return c.pass()
	}
	return c.fail("README has no headings (expected at least one # heading or underline heading)")
}

func hasMarkdownHeading(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
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
