// Package output renders scan results as a console summary and as JSON.
package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"

	"github.com/baselinerhq/baseliner/internal/models"
)

const slugWidth = 40

// PrintSummary writes the per-repo table, the critical/high failures block, and
// the footer — matching the Python console output (modulo ANSI color codes).
func PrintSummary(w io.Writer, r *models.RunResult) {
	printTable(w, r)
	printFailures(w, r)
	printFooter(w, r)
}

// notAssessed renders an undefined posture — nothing conclusive was observed.
var notAssessed = color.New(color.FgMagenta)

func scoreColor(score float64) *color.Color {
	switch {
	case score >= 0.8:
		return color.New(color.FgGreen)
	case score >= 0.5:
		return color.New(color.FgYellow)
	default:
		return color.New(color.FgRed)
	}
}

func counts(repo models.RepoResult) (pass, fail, unknown int) {
	for _, c := range repo.Results {
		switch c.Status {
		case models.StatusPass:
			pass++
		case models.StatusFail, models.StatusError:
			fail++
		case models.StatusUnknown:
			unknown++
		}
	}
	return
}

func printTable(w io.Writer, r *models.RunResult) {
	multi := multiForge(r)
	if multi {
		fmt.Fprintf(w, "%-*s  %-6s  %5s  %5s  %5s  %5s  %5s\n", slugWidth, "repo", "forge", "score", "cover", "pass", "fail", "unk")
		fmt.Fprintln(w, strings.Repeat("-", slugWidth+44))
	} else {
		fmt.Fprintf(w, "%-*s  %5s  %5s  %5s  %5s  %5s\n", slugWidth, "repo", "score", "cover", "pass", "fail", "unk")
		fmt.Fprintln(w, strings.Repeat("-", slugWidth+36))
	}
	for _, repo := range r.Repos {
		pass, fail, unknown := counts(repo)
		scoreStr := notAssessed.Sprintf("%5s", "n/a")
		if posture, ok := repo.Posture(); ok {
			scoreStr = scoreColor(posture).Sprintf("%5.2f", posture)
		}
		name := fmt.Sprintf("%-*s", slugWidth, truncate(repo.Slug, slugWidth))
		if multi {
			name += fmt.Sprintf("  %-6s", repo.Forge)
		}
		fmt.Fprintf(w, "%s  %s  %4.0f%%  %5d  %5d  %5d\n",
			name, scoreStr, float64(repo.Coverage)*100, pass, fail, unknown)
	}
}

func printFailures(w io.Writer, r *models.RunResult) {
	anyPrinted := false
	for _, repo := range r.Repos {
		var crit []models.CheckResult
		for _, c := range repo.Results {
			if (c.Status == models.StatusFail || c.Status == models.StatusError) &&
				(c.Severity == models.SeverityCritical || c.Severity == models.SeverityHigh) {
				crit = append(crit, c)
			}
		}
		if len(crit) == 0 {
			continue
		}
		if !anyPrinted {
			fmt.Fprintln(w, "")
			fmt.Fprintln(w, "Critical/high failures:")
			anyPrinted = true
		}
		if multiForge(r) {
			fmt.Fprintf(w, "  %s (%s)\n", repo.Slug, repo.Forge)
		} else {
			fmt.Fprintf(w, "  %s\n", repo.Slug)
		}
		for _, c := range crit {
			sevColor := color.New(color.FgYellow)
			if c.Severity == models.SeverityCritical {
				sevColor = color.New(color.FgRed)
			}
			sev := sevColor.Sprint(strings.ToUpper(string(c.Severity)))
			msg := "(no message)"
			if c.Message != nil {
				msg = *c.Message
			}
			fmt.Fprintf(w, "    [%s] %s: %s\n", sev, c.CheckID, msg)
			if c.PolicyInfo != "" {
				fmt.Fprintf(w, "        %s\n", c.PolicyInfo)
			}
			if c.PolicyURL != "" {
				fmt.Fprintf(w, "        see %s\n", c.PolicyURL)
			}
		}
	}
}

func printFooter(w io.Writer, r *models.RunResult) {
	fmt.Fprintln(w, "")
	failColor := color.New(color.FgRed)
	if r.Failed == 0 {
		failColor = color.New(color.FgGreen)
	}
	failStr := failColor.Sprintf("%d", r.Failed)
	fmt.Fprintf(w, "%d repos scanned — %d passed, %s failed\n", r.TotalRepos, r.Passed, failStr)
	if r.Privacy != nil {
		verb := "redacted from"
		if r.Privacy.Mode == "exclude" {
			verb = "hidden from"
		}
		fmt.Fprintf(w, "%d private repo(s) %s public output.\n", r.Privacy.Count, verb)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// multiForge reports whether the run's repos come from more than one forge,
// when the output shows each repo's forge: a slug alone could name two repos.
func multiForge(r *models.RunResult) bool {
	seen := ""
	for _, repo := range r.Repos {
		if repo.Forge == "" {
			continue
		}
		if seen != "" && repo.Forge != seen {
			return true
		}
		seen = repo.Forge
	}
	return false
}
