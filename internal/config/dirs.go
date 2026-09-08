package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/droxey/x3vault/internal/pathutil"
)

const (
	WikiModeAllExceptIgnored = "all_except_ignored"
	WikiModeWhitelist        = "whitelist"
)

// LLMWikiStandardDirs are the conventional LLM Wiki folders used for init hints.
// See https://github.com/droxey/x3vault and the Karpathy LLM Wiki pattern.
var LLMWikiStandardDirs = []string{
	"sources",
	"entities",
	"concepts",
	"analyses",
}

// LLMWikiDefaults is the default wiki directory policy for github.com/droxey/x3vault.
var LLMWikiDefaults = WikiDirs{
	Mode: WikiModeAllExceptIgnored,
	Ignored: []string{
		"script",
		"references",
	},
}

// WikiDirs controls which subdirectories under source_root (wiki/) are synced.
// Default mode syncs all subdirectories except ignored_dirs. Root-level *.md is always included.
// Vault-relative exclusions are applied separately during discovery.
type WikiDirs struct {
	Mode         string   `yaml:"mode"`
	Allowed      []string `yaml:"allowed_dirs,omitempty"`
	Ignored      []string `yaml:"ignored_dirs"`
	StandardDirs []string `yaml:"standard_dirs"`
}

func DefaultWikiDirs() WikiDirs {
	return WikiDirs{
		Mode:         LLMWikiDefaults.Mode,
		Ignored:      append([]string(nil), LLMWikiDefaults.Ignored...),
		StandardDirs: append([]string(nil), LLMWikiStandardDirs...),
	}
}

func (w *WikiDirs) Normalize() {
	w.Mode = strings.TrimSpace(w.Mode)
	if w.Mode == "" {
		w.Mode = WikiModeAllExceptIgnored
	}
	w.Allowed = normalizeDirList(w.Allowed)
	w.Ignored = normalizeDirList(w.Ignored)
}

func (w *WikiDirs) RestoreDefaults() {
	*w = DefaultWikiDirs()
}

func (w *WikiDirs) Validate() error {
	switch w.Mode {
	case WikiModeAllExceptIgnored, WikiModeWhitelist:
	default:
		return fmt.Errorf("unsupported wiki.mode %q (want %q or %q)", w.Mode, WikiModeAllExceptIgnored, WikiModeWhitelist)
	}
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
	for _, d := range w.StandardDirs {
		if err := validateDirEntry(d, "standard_dirs"); err != nil {
			return err
		}
	}
	if w.Mode == WikiModeWhitelist && len(w.Allowed) == 0 {
		return fmt.Errorf("wiki.mode %q requires at least one allowed_dirs entry", WikiModeWhitelist)
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
	if err := pathutil.ValidateRelative(dir); err != nil {
		return fmt.Errorf("%s entry %q: %w", field, dir, err)
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
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func cleanDirEntry(dir string) string {
	return strings.TrimSpace(dir)
}

func (w *WikiDirs) ShouldIncludeRelPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if rel == "." || !strings.Contains(rel, "/") {
		return true
	}
	if w.isIgnored(rel) {
		return false
	}
	if w.Mode == WikiModeAllExceptIgnored {
		return true
	}
	return w.isAllowed(rel)
}

func (w *WikiDirs) ShouldWalkDir(relDir string) bool {
	relDir = cleanDirEntry(filepath.ToSlash(relDir))
	if relDir == "" || relDir == "." {
		return true
	}
	if w.isIgnored(relDir) {
		return false
	}
	if w.Mode == WikiModeAllExceptIgnored {
		return true
	}
	for _, al := range w.Allowed {
		if dirRelevant(relDir, al) {
			return true
		}
	}
	return false
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

func dirRelevant(relDir, allowed string) bool {
	allowed = cleanDirEntry(allowed)
	relDir = cleanDirEntry(relDir)
	if allowed == "" {
		return false
	}
	return dirMatches(relDir, allowed) || dirMatches(allowed, relDir)
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

// MissingStandardDirs returns LLM Wiki folders absent under wikiPath (for init hints).
func MissingStandardDirs(wikiPath string, standardDirs []string) []string {
	var missing []string
	for _, d := range standardDirs {
		p := filepath.Join(wikiPath, d)
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			missing = append(missing, d)
		}
	}
	return missing
}
