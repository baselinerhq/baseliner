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

func counts(repo models.RepoResult) (pass, fail, skip int) {
	for _, c := range repo.Results {
		switch c.Status {
		case models.StatusPass:
			pass++
		case models.StatusFail, models.StatusError:
			fail++
		case models.StatusSkip:
			skip++
		}
	}
	return
}

func printTable(w io.Writer, r *models.RunResult) {
	fmt.Fprintf(w, "%-*s  %5s  %5s  %5s  %5s\n", slugWidth, "repo", "score", "pass", "fail", "skip")
	fmt.Fprintln(w, strings.Repeat("-", slugWidth+28))
	for _, repo := range r.Repos {
		pass, fail, skip := counts(repo)
		score := float64(repo.Score)
		scoreStr := scoreColor(score).Sprintf("%5.2f", score)
		fmt.Fprintf(w, "%-*s  %s  %5d  %5d  %5d\n", slugWidth, truncate(repo.Slug, slugWidth), scoreStr, pass, fail, skip)
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
		fmt.Fprintf(w, "  %s\n", repo.Slug)
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
