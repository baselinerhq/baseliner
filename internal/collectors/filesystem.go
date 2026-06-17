package collectors

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
)

const maxReadmeBytes = 4096

// Filesystem collects a FilesystemContext from a local directory.
type Filesystem struct{}

// Collect walks the source path and builds a NormalizedRepository with fs context.
// A missing path or read error yields an empty (all-absent) context, never an error.
func (Filesystem) Collect(src source.Repo) *models.NormalizedRepository {
	if src.Path == "" {
		slog.Warn("filesystem collector called without a path", "slug", src.Slug)
		return emptyResult(src)
	}
	root, err := filepath.Abs(src.Path)
	if err != nil {
		return emptyResult(src)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		slog.Warn("path missing or not a directory", "path", root)
		return emptyResult(src)
	}

	files := collectFiles(root)
	readme := readReadme(root, files)

	return &models.NormalizedRepository{
		SourceType: models.SourceType(src.Type),
		Slug:       src.Slug,
		Name:       filepath.Base(root),
		FS: &models.FilesystemContext{
			Files:          files,
			KeyFiles:       DetectKeyFiles(files),
			ReadmeContent:  readme,
			CIFiles:        DetectCIFiles(files),
			DepUpdateFiles: DetectDependencyUpdateFiles(files),
		},
	}
}

// collectFiles returns sorted, deduped relative POSIX paths up to depth 4,
// excluding the .git directory.
func collectFiles(root string) []string {
	seen := map[string]bool{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walk error", "path", p, "err", err)
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		relPosix := filepath.ToSlash(rel)

		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			if relPosix != "." && len(strings.Split(relPosix, "/")) >= 4 {
				return filepath.SkipDir // stop descending; deeper files would exceed 4 parts
			}
			return nil
		}
		if relPosix == "." {
			return nil
		}
		parts := strings.Split(relPosix, "/")
		if parts[0] == ".git" || len(parts) > 4 {
			return nil
		}
		// A symlink to a directory is a "dirname" under os.walk(followlinks=False),
		// not a file — WalkDir reports it as a non-dir entry, so skip it here to
		// avoid listing a phantom file. Symlinks to files (and broken links) are
		// kept, matching Python.
		if d.Type()&fs.ModeSymlink != 0 {
			if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
				return nil
			}
		}
		seen[relPosix] = true
		return nil
	})

	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// readReadme reads the first README's first 4096 bytes as UTF-8 (invalid bytes
// replaced). Returns nil if no README or it cannot be read.
func readReadme(root string, files []string) *string {
	rel := FindReadmePath(files)
	if rel == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		slog.Warn("could not read README", "path", rel, "err", err)
		return nil
	}
	if len(data) > maxReadmeBytes {
		data = data[:maxReadmeBytes]
	}
	s := strings.ToValidUTF8(string(data), "�")
	return &s
}

func emptyResult(src source.Repo) *models.NormalizedRepository {
	return &models.NormalizedRepository{
		SourceType: models.SourceType(src.Type),
		Slug:       src.Slug,
		Name:       resolveName(src),
		FS: &models.FilesystemContext{
			Files:          []string{},
			KeyFiles:       emptyKeyFiles(),
			ReadmeContent:  nil,
			CIFiles:        []string{},
			DepUpdateFiles: []string{},
		},
	}
}

func resolveName(src source.Repo) string {
	if src.Path != "" {
		return filepath.Base(src.Path)
	}
	if i := strings.LastIndex(src.Slug, "/"); i >= 0 {
		return src.Slug[i+1:]
	}
	return src.Slug
}
