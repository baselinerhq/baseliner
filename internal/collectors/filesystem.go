package collectors

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/baselinerhq/baseliner/internal/models"
	"github.com/baselinerhq/baseliner/internal/source"
	"github.com/baselinerhq/baseliner/internal/waivers"
)

const maxReadmeBytes = 4096

// Filesystem collects a FilesystemContext from a local directory.
type Filesystem struct {
	// ExtraDirs are directories a policy's file_present checks need beyond
	// evidenceDirs; the walk reads them, and this records them as unread
	// when an ancestor could not be read.
	ExtraDirs []string
}

// Collect walks the source path and builds a NormalizedRepository with fs context.
// A missing path or read error yields an empty (all-absent) context, never an error.
func (f Filesystem) Collect(src source.Repo) *models.NormalizedRepository {
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

	files, failed := collectFiles(root)
	var unread, policyUnread []string
	for _, d := range failed {
		unread = append(unread, unreadBelow(d, evidenceDirs)...)
		policyUnread = append(policyUnread, unreadBelow(d, f.ExtraDirs)...)
	}
	readme, readmeOK := readReadme(root, files)
	if FindReadmePath(files) == "" && len(unread) > 0 {
		// No README was listed, but one could be in a directory the walk
		// could not read, so its absence is not shown.
		readmeOK = false
	}

	return &models.NormalizedRepository{
		SourceType: models.SourceType(src.Type),
		Slug:       src.Slug,
		Waivers:    localWaivers(root, files, src.Slug),
		Name:       filepath.Base(root),
		FS: &models.FilesystemContext{
			Files:          files,
			KeyFiles:       DetectKeyFiles(files),
			ReadmeContent:  readme,
			CIFiles:        DetectCIFiles(files),
			DepUpdateFiles: DetectDependencyUpdateFiles(files),
			UnreadDirs:     unread,
			ReadmeUnread:   !readmeOK,
			// The walk reads every directory, so a policy's checks see the
			// same files.
			PolicyFiles:      files,
			PolicyUnreadDirs: policyUnread,
		},
	}
}

// evidenceDirs are the directories the GitHub collector lists, and the ones
// the checks scope their evidence to.
var evidenceDirs = []string{"", ".github", ".github/workflows", ".circleci", "docs"}

// listing gathers a forge's directory listings. A directory in base is
// evidence for the built-in checks and one in extra for a policy's
// file_present checks; a directory in both counts for both. Keeping them
// apart means a policy listing more directories never changes a built-in
// check: a LICENSE or README in one of them is not the repo's.
type listing struct {
	base, extra               []string
	files, unread             []string
	policyFiles, policyUnread []string
}

func newListing(base, extra []string) *listing {
	return &listing{base: base, extra: extra}
}

// dirs returns every directory to list, base first, so a forge that reads
// the root to prove the ref reads it before any other.
func (l *listing) dirs() []string { return withExtra(l.base, l.extra) }

// add records dir's listing: its files, or that it could not be read.
func (l *listing) add(dir string, got []string, ok bool) {
	if slices.Contains(l.base, dir) {
		l.files = append(l.files, got...)
		if !ok {
			l.unread = append(l.unread, dir)
		}
	}
	if slices.Contains(l.extra, dir) {
		l.policyFiles = append(l.policyFiles, got...)
		if !ok {
			l.policyUnread = append(l.policyUnread, dir)
		}
	}
}

// unreadAll records that no directory could be read.
func (l *listing) unreadAll() {
	for _, d := range l.dirs() {
		l.add(d, nil, false)
	}
}

// withExtra returns dirs followed by each of extra not already in it.
func withExtra(dirs, extra []string) []string {
	out := slices.Clone(dirs)
	for _, d := range extra {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// unreadBelow returns d and each of dirs beneath it: one walk covers the
// tree, so nothing under an unreadable directory was seen.
func unreadBelow(d string, dirs []string) []string {
	out := []string{d}
	for _, e := range dirs {
		if e != d && (d == "" || strings.HasPrefix(e, d+"/")) {
			out = append(out, e)
		}
	}
	return out
}

// collectFiles returns sorted, deduped relative POSIX paths up to depth 4,
// excluding the .git directory, and the directories (relative, "" for the
// root) that could not be read, so their contents are unknown rather than
// absent.
func collectFiles(root string) ([]string, []string) {
	seen := map[string]bool{}
	var failed []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walk error", "path", p, "err", err)
			if rel, relErr := filepath.Rel(root, p); relErr == nil && (d == nil || d.IsDir()) {
				if rel = filepath.ToSlash(rel); rel == "." {
					rel = ""
				}
				failed = append(failed, rel)
			}
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
	return out, failed
}

// waiverFile returns the waiver file name the listing has, or "" when it has
// none or, ambiguously, more than one.
func waiverFile(files []string, slug string) string {
	var found []string
	for _, n := range waivers.Names {
		if slices.Contains(files, n) {
			found = append(found, n)
		}
	}
	if len(found) > 1 {
		slog.Warn("ignoring repo waivers: both .baseliner.yml and .baseliner.yaml exist", "repo", slug)
		return ""
	}
	if len(found) == 0 {
		return ""
	}
	return found[0]
}

// localWaivers reads the repo's waiver file when the walk listed one. It must
// be a regular file in the repo: a symlink, which could point outside it, or
// a device or pipe, which could block the scan, is refused. A file that cannot
// be read or parsed declares no waivers: the checks run as they would without
// it, and a warning says why.
func localWaivers(root string, files []string, slug string) []models.Waiver {
	name := waiverFile(files, slug)
	if name == "" {
		return nil
	}
	p := filepath.Join(root, name)
	if info, err := os.Lstat(p); err != nil || !info.Mode().IsRegular() {
		slog.Warn("ignoring repo waivers: not a regular file", "repo", slug, "file", name)
		return nil
	}
	// Open without following a link or blocking on a pipe, and check what was
	// opened: the file can change between the Lstat and the open.
	f, err := openRegular(p)
	if err != nil {
		slog.Warn("ignoring repo waivers: not a regular file or could not be read", "repo", slug, "file", name)
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, waivers.MaxBytes+1))
	if err != nil {
		slog.Warn("ignoring repo waivers: file could not be read", "repo", slug, "file", name)
		return nil
	}
	ws, err := waivers.Parse(data)
	if err != nil {
		slog.Warn("ignoring repo waivers", "repo", slug, "file", name, "err", err)
		return nil
	}
	return ws
}

// readReadme reads the first README's first 4096 bytes as UTF-8 (invalid bytes
// replaced). It returns nil if there is no README, and false if one is listed
// but cannot be read.
func readReadme(root string, files []string) (*string, bool) {
	rel := FindReadmePath(files)
	if rel == "" {
		return nil, true
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		slog.Warn("could not read README", "path", rel, "err", err)
		return nil, false
	}
	if len(data) > maxReadmeBytes {
		data = data[:maxReadmeBytes]
	}
	s := strings.ToValidUTF8(string(data), "�")
	return &s, true
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
