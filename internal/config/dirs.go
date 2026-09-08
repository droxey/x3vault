package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LLMWikiDefaults follows the Karpathy LLM Wiki layout documented in the
// official pattern (wiki/index.md, sources/, entities/, concepts/, analyses/)
// and implemented by github.com/microsoft/llmwiki.
var LLMWikiDefaults = WikiDirs{
	Allowed: []string{
		"sources",
		"entities",
		"concepts",
		"analyses",
	},
	Ignored: []string{
		"script",
		"references",
	},
}

// WikiDirs controls which subdirectories under source_root (typically wiki/)
// are included in build/sync. Root-level *.md files are always included.
type WikiDirs struct {
	Allowed []string `yaml:"allowed_dirs"`
	Ignored []string `yaml:"ignored_dirs"`
}

func DefaultWikiDirs() WikiDirs {
	return WikiDirs{
		Allowed: append([]string(nil), LLMWikiDefaults.Allowed...),
		Ignored: append([]string(nil), LLMWikiDefaults.Ignored...),
	}
}

func (w *WikiDirs) Normalize() {
	w.Allowed = normalizeDirList(w.Allowed)
	w.Ignored = normalizeDirList(w.Ignored)
}

func (w *WikiDirs) RestoreDefaults() {
	w.Allowed = append([]string(nil), LLMWikiDefaults.Allowed...)
	w.Ignored = append([]string(nil), LLMWikiDefaults.Ignored...)
}

func (w *WikiDirs) Validate() error {
	for _, d := range w.Allowed {
		if err := validateDirEntry(d, "allowed_dirs"); err != nil {
			return err
		}
	}
	for _, d := range w.Ignored {
		if err := validateDirEntry(d, "ignored_dirs"); err != nil {
			return err
		}
	}
	for _, a := range w.Allowed {
		for _, ig := range w.Ignored {
			if dirMatches(a, ig) || dirMatches(ig, a) {
				return fmt.Errorf("directory %q appears in both allowed_dirs and ignored_dirs", a)
			}
		}
	}
	return nil
}

func validateDirEntry(dir, field string) error {
	dir = cleanDirEntry(dir)
	if dir == "" || dir == "." {
		return fmt.Errorf("%s entry must be a non-empty relative path", field)
	}
	if filepath.IsAbs(dir) {
		return fmt.Errorf("%s entry %q must be relative to source_root", field, dir)
	}
	if strings.HasPrefix(dir, ".") {
		return fmt.Errorf("%s entry %q must not start with '.'", field, dir)
	}
	return nil
}

func normalizeDirList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = cleanDirEntry(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func cleanDirEntry(dir string) string {
	dir = strings.TrimSpace(dir)
	dir = strings.Trim(dir, "/")
	dir = filepath.ToSlash(dir)
	return dir
}

// ShouldIncludeRelPath reports whether a note path relative to source_root
// should be discovered. Root-level markdown files are always included.
func (w *WikiDirs) ShouldIncludeRelPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if rel == "." || !strings.Contains(rel, "/") {
		return true
	}
	if w.isIgnored(rel) {
		return false
	}
	if len(w.Allowed) == 0 {
		return true
	}
	return w.isAllowed(rel)
}

func (w *WikiDirs) isIgnored(rel string) bool {
	for _, ig := range w.Ignored {
		if dirMatches(rel, ig) {
			return true
		}
	}
	return false
}

func (w *WikiDirs) isAllowed(rel string) bool {
	for _, al := range w.Allowed {
		if dirMatches(rel, al) {
			return true
		}
	}
	return false
}

// ShouldWalkDir reports whether to descend into a subdirectory of source_root.
// The source root itself is always walked.
func (w *WikiDirs) ShouldWalkDir(relDir string) bool {
	relDir = cleanDirEntry(relDir)
	if relDir == "" || relDir == "." {
		return true
	}
	if w.isIgnored(relDir) {
		return false
	}
	if len(w.Allowed) == 0 {
		return true
	}
	for _, al := range w.Allowed {
		if dirRelevant(relDir, al) {
			return true
		}
	}
	return false
}

func dirRelevant(relDir, allowed string) bool {
	allowed = cleanDirEntry(allowed)
	relDir = cleanDirEntry(relDir)
	if allowed == "" {
		return false
	}
	if dirMatches(relDir, allowed) || dirMatches(allowed, relDir) {
		return true
	}
	return strings.HasPrefix(allowed, relDir+"/")
}

func dirMatches(rel, pattern string) bool {
	pattern = cleanDirEntry(pattern)
	rel = cleanDirEntry(rel)
	if pattern == "" {
		return false
	}
	if rel == pattern {
		return true
	}
	return strings.HasPrefix(rel, pattern+"/")
}
