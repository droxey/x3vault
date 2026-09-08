package vault

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AssertBuildWritePath ensures build output is written only under buildRoot.
// x3vault must never mutate Obsidian vault content (wiki/, attachments, .obsidian/).
func AssertBuildWritePath(path, buildRoot string) error {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve write path: %w", err)
	}
	rootAbs, err := filepath.Abs(buildRoot)
	if err != nil {
		return fmt.Errorf("resolve build root: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("refusing to write outside build output %s: %s", buildRoot, path)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write outside build output %s: %s", buildRoot, path)
	}
	return nil
}

// IsObsidianManagedPath reports whether absPath is Obsidian-managed vault content
// that x3vault must treat as read-only (wiki source, Obsidian config, attachments).
func IsObsidianManagedPath(absPath, vaultRoot, sourceRel string) bool {
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return false
	}
	vaultRoot, err = filepath.Abs(vaultRoot)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(vaultRoot, absPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	if strings.HasPrefix(rel, ".obsidian/") || rel == ".obsidian" {
		return true
	}
	sourceRel = filepath.ToSlash(strings.TrimSpace(sourceRel))
	if sourceRel != "" && (rel == sourceRel || strings.HasPrefix(rel, sourceRel+"/")) {
		return true
	}
	return false
}
