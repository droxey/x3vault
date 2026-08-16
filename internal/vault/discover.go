package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func Discover(vaultRoot, sourceRel string) (*Discovery, error) {
	sourceAbs := filepath.Join(vaultRoot, sourceRel)
	info, err := os.Stat(sourceAbs)
	if err != nil {
		return nil, fmt.Errorf("source dir %s: %w", sourceAbs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source is not a directory: %s", sourceAbs)
	}

	// Canonicalize to detect escapes
	vaultCanon, err := filepath.EvalSymlinks(vaultRoot)
	if err != nil {
		vaultCanon = vaultRoot
	}
	sourceCanon, err := filepath.EvalSymlinks(sourceAbs)
	if err != nil {
		sourceCanon = sourceAbs
	}
	if !strings.HasPrefix(sourceCanon, vaultCanon) {
		return nil, fmt.Errorf("source escapes vault root")
	}

	var notes []Note
	err = filepath.WalkDir(sourceAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
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
