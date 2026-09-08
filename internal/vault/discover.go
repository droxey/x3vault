package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/pathutil"
)

type Note struct {
	RelPath string // slash-separated, relative to source root (wiki/)
	AbsPath string
}

type Discovery struct {
	VaultRoot  string
	SourceRoot string // absolute path to wiki/
	Notes      []Note
}

// Discover finds regular Markdown notes, enforcing wiki rules and optional
// vault-relative exclusions. It never follows symlinked source directories.
func Discover(vaultRoot, sourceRel string, dirs config.WikiDirs, excludePaths ...[]string) (*Discovery, error) {
	if err := pathutil.ValidateRelative(sourceRel); err != nil {
		return nil, fmt.Errorf("source_root: %w", err)
	}
	dirs.Normalize()
	if err := dirs.Validate(); err != nil {
		return nil, err
	}
	exclusions := config.Config{}
	for _, paths := range excludePaths {
		for _, p := range paths {
			if err := pathutil.ValidateRelative(p); err != nil {
				return nil, fmt.Errorf("excluded vault path %q: %w", p, err)
			}
		}
		exclusions.Sync.ExcludeVaultPaths = append(exclusions.Sync.ExcludeVaultPaths, paths...)
	}
	vaultCanon, err := pathutil.Canonical(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("vault root: %w", err)
	}
	sourceAbs := vaultCanon
	for _, segment := range strings.Split(sourceRel, "/") {
		sourceAbs = filepath.Join(sourceAbs, segment)
		info, err := os.Lstat(sourceAbs)
		if err != nil {
			return nil, fmt.Errorf("source dir %s: %w", sourceAbs, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("source must be a directory without symlinks: %s", sourceAbs)
		}
	}

	var notes []Note
	err = filepath.WalkDir(sourceAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		vaultRel, err := filepath.Rel(vaultCanon, path)
		if err != nil {
			return err
		}
		if exclusions.IsExcludedVaultPath(filepath.ToSlash(vaultRel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if path != sourceAbs {
				relDir, err := filepath.Rel(sourceAbs, path)
				if err != nil {
					return err
				}
				if !dirs.ShouldWalkDir(relDir) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !dirs.ShouldIncludeRelPath(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("note must be a regular file without symlinks: %s", path)
		}
		canonical, err := pathutil.Canonical(path)
		if err != nil {
			return err
		}
		if !pathutil.ContainedIn(canonical, vaultCanon) {
			return fmt.Errorf("note escapes vault: %s", path)
		}
		canonicalRel, err := filepath.Rel(vaultCanon, canonical)
		if err != nil {
			return err
		}
		if exclusions.IsExcludedVaultPath(filepath.ToSlash(canonicalRel)) {
			return nil
		}
		notes = append(notes, Note{
			RelPath: rel,
			AbsPath: path,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source: %w", err)
	}

	sort.Slice(notes, func(i, j int) bool {
		return notes[i].RelPath < notes[j].RelPath
	})

	return &Discovery{
		VaultRoot:  vaultRoot,
		SourceRoot: sourceAbs,
		Notes:      notes,
	}, nil
}
