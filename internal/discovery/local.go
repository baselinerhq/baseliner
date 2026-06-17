// Package discovery finds repositories to scan from local paths or GitHub.
package discovery

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/baselinerhq/baseliner/internal/source"
)

// Local discovers repositories from filesystem paths.
type Local struct {
	Paths []string
}

// Discover resolves each path (expanding ~ and symlinks), skipping any that don't
// exist or aren't directories. The slug is the resolved absolute path — the key
// used by policy.repo_ignores for local repos.
func (d Local) Discover() []source.Repo {
	var sources []source.Repo
	for _, raw := range d.Paths {
		abs := resolvePath(raw)
		info, err := os.Stat(abs)
		if err != nil {
			slog.Warn("local path does not exist, skipping", "path", abs)
			continue
		}
		if !info.IsDir() {
			slog.Warn("local path is not a directory, skipping", "path", abs)
			continue
		}
		sources = append(sources, source.Repo{Type: "local", Slug: abs, Path: abs})
	}
	return sources
}

func resolvePath(p string) string {
	p = expandUser(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
