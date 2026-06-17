package output

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/version"
)

// SARIF 2.1.0 output, suitable for upload to GitHub code scanning.
// https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ShortDescription     sarifText         `json:"shortDescription"`
	DefaultConfiguration sarifConfig       `json:"defaultConfiguration"`
	HelpURI              string            `json:"helpUri"`
	Properties           map[string]string `json:"properties"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    sarifText         `json:"message"`
	Locations  []sarifLocation   `json:"locations"`
	Properties map[string]string `json:"properties"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

// ruleMeta describes a check as a SARIF rule. The default level mirrors the
// built-in default policy; per-result levels come from the actual run.
type ruleMeta struct {
	id          string
	name        string
	description string
	defLevel    string
}

// sarifRules is the catalog of checks as SARIF rules. The descriptions state the
// passing condition. Kept in sync with the default policy (a test asserts 10).
var sarifRules = []ruleMeta{
	{"readme_exists", "ReadmeExists", "Repository has a README file.", "error"},
	{"readme_nonempty", "ReadmeNonEmpty", "README is present and non-empty.", "error"},
	{"readme_has_heading", "ReadmeHasHeading", "README has at least one heading.", "warning"},
	{"license_exists", "LicenseExists", "Repository has a LICENSE or COPYING file.", "error"},
	{"gitignore_exists", "GitignoreExists", "Repository has a .gitignore.", "warning"},
	{"ci_present", "CiPresent", "Repository has at least one CI workflow.", "error"},
	{"codeowners_exists", "CodeownersExists", "Repository has a CODEOWNERS file.", "note"},
	{"dependency_update_config", "DependencyUpdateConfig", "Repository has a Dependabot or Renovate config.", "warning"},
	{"default_branch_is_main", "DefaultBranchIsMain", "Default branch is 'main'.", "warning"},
	{"stale_repo", "StaleRepo", "Repository has recent commit activity.", "note"},
}

const sarifHelpURI = "https://github.com/baselinerhq/baseliner/blob/main/docs/policies.md"

// severityToLevel maps a baseliner severity to a SARIF level.
func severityToLevel(sev models.Severity) string {
	switch sev {
	case models.SeverityCritical, models.SeverityHigh:
		return "error"
	case models.SeverityMedium:
		return "warning"
	case models.SeverityLow:
		return "note"
	default:
		return "warning"
	}
}

// buildSARIF renders a run as a SARIF log: one rule per built-in check and one
// result per failing/errored check (passes and skips are not reported).
func buildSARIF(r *models.RunResult) sarifLog {
	rules := make([]sarifRule, 0, len(sarifRules))
	for _, m := range sarifRules {
		rules = append(rules, sarifRule{
			ID:                   m.id,
			Name:                 m.name,
			ShortDescription:     sarifText{Text: m.description},
			DefaultConfiguration: sarifConfig{Level: m.defLevel},
			HelpURI:              sarifHelpURI,
			Properties:           map[string]string{"category": "baseline"},
		})
	}

	var results []sarifResult
	for _, repo := range r.Repos {
		for _, c := range repo.Results {
			if c.Status != models.StatusFail && c.Status != models.StatusError {
				continue
			}
			msg := c.CheckID
			if c.Message != nil {
				msg = *c.Message
			}
			results = append(results, sarifResult{
				RuleID:  c.CheckID,
				Level:   severityToLevel(c.Severity),
				Message: sarifText{Text: msg},
				Locations: []sarifLocation{{
					PhysicalLocation: sarifPhysical{
						ArtifactLocation: sarifArtifact{URI: repo.Slug},
					},
				}},
				Properties: map[string]string{
					"repo":     repo.Slug,
					"severity": string(c.Severity),
					"status":   string(c.Status),
				},
			})
		}
	}

	return sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "baseliner",
				InformationURI: "https://github.com/baselinerhq/baseliner",
				Version:        version.Version,
				Rules:          rules,
			}},
			Results: results,
		}},
	}
}

// WriteSARIF writes the run as a SARIF 2.1.0 file at path (atomic tmp + rename).
func WriteSARIF(r *models.RunResult, path string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildSARIF(r)); err != nil {
		return err
	}
	content := bytes.TrimRight(buf.Bytes(), "\n")

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
