// Package collectors builds NormalizedRepository values from local filesystem,
// local git, and the GitHub API. The detection functions here are shared by the
// filesystem and GitHub-API collectors so both classify files identically.
package collectors

import (
	"path"
	"sort"
	"strings"
)

var readmeNames = map[string]bool{"readme.md": true, "readme.rst": true, "readme.txt": true, "readme": true}
var licenseNames = map[string]bool{"license": true, "license.md": true, "license.txt": true, "copying": true}

var depUpdatePaths = map[string]bool{
	".github/dependabot.yml":  true,
	".github/dependabot.yaml": true,
	"renovate.json":           true,
	".renovaterc":             true,
	".renovaterc.json":        true,
}

// DetectKeyFiles flags README/LICENSE/GITIGNORE/CODEOWNERS presence from a list
// of relative POSIX paths. CODEOWNERS must sit in the repo root or .github.
func DetectKeyFiles(files []string) map[string]bool {
	kf := map[string]bool{"README": false, "LICENSE": false, "GITIGNORE": false, "CODEOWNERS": false}
	for _, rel := range files {
		name := strings.ToLower(path.Base(rel))
		parent := strings.ToLower(path.Dir(rel))
		if readmeNames[name] {
			kf["README"] = true
		}
		if licenseNames[name] {
			kf["LICENSE"] = true
		}
		if name == ".gitignore" {
			kf["GITIGNORE"] = true
		}
		if name == "codeowners" && (parent == "." || parent == "" || parent == ".github") {
			kf["CODEOWNERS"] = true
		}
	}
	return kf
}

// DetectCIFiles returns CI workflow files (sorted, deduped).
func DetectCIFiles(files []string) []string {
	set := map[string]bool{}
	for _, rel := range files {
		low := strings.ToLower(rel)
		name := strings.ToLower(path.Base(rel))
		switch {
		case strings.HasPrefix(low, ".github/workflows/") && (strings.HasSuffix(low, ".yml") || strings.HasSuffix(low, ".yaml")):
			set[rel] = true
		case low == ".circleci/config.yml":
			set[rel] = true
		case name == "jenkinsfile":
			set[rel] = true
		case low == ".gitlab-ci.yml":
			set[rel] = true
		}
	}
	return sortedKeys(set)
}

// DetectDependencyUpdateFiles returns Dependabot/Renovate configs (sorted, deduped).
func DetectDependencyUpdateFiles(files []string) []string {
	set := map[string]bool{}
	for _, rel := range files {
		if depUpdatePaths[strings.ToLower(rel)] {
			set[rel] = true
		}
	}
	return sortedKeys(set)
}

// FindReadmePath returns the first README-like path, or "" if none.
func FindReadmePath(files []string) string {
	for _, rel := range files {
		if readmeNames[strings.ToLower(path.Base(rel))] {
			return rel
		}
	}
	return ""
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func emptyKeyFiles() map[string]bool {
	return map[string]bool{"README": false, "LICENSE": false, "GITIGNORE": false, "CODEOWNERS": false}
}
