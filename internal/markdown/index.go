package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type NoteRef struct {
	RelPath string
	AbsPath string
}

type IndexResult struct {
	Index    map[string]string
	Warnings []string
}

// BuildNoteIndex resolves Obsidian-style wikilink targets to note paths.
// Keys include full path, path without extension, basename, title, and aliases.
func BuildNoteIndex(notes []NoteRef) IndexResult {
	res := IndexResult{Index: make(map[string]string)}
	add := func(key, rel, kind string) {
		key = normalizeIndexKey(key)
		if key == "" {
			return
		}
		if existing, ok := res.Index[key]; ok && existing != rel {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"ambiguous %s %q maps to both %s and %s", kind, key, existing, rel))
			return
		}
		res.Index[key] = rel
	}

	for _, n := range notes {
		rel := filepath.ToSlash(n.RelPath)
		add(rel, rel, "path")
		add(strings.TrimSuffix(rel, ".md"), rel, "path")
		add(strings.TrimSuffix(filepath.Base(rel), ".md"), rel, "basename")

		raw, err := os.ReadFile(n.AbsPath)
		if err != nil {
			continue
		}
		text := string(raw)
		if m := reFrontmatter.FindStringSubmatch(text); m != nil {
			fm := m[1]
			title := extractYAMLString(fm, "title")
			if title != "" {
				add(title, rel, "title")
			}
			for _, alias := range extractYAMLList(fm, "aliases") {
				add(alias, rel, "alias")
			}
			for _, alias := range extractYAMLBlockList(fm, "aliases") {
				add(alias, rel, "alias")
			}
		}
	}
	return res
}

func normalizeIndexKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.TrimSuffix(key, ".md")
	key = filepath.ToSlash(key)
	return key
}

func extractYAMLBlockList(fm, key string) []string {
	lines := strings.Split(fm, "\n")
	var out []string
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, key+":") && !strings.Contains(trimmed, "[") {
				inBlock = true
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		item = strings.Trim(item, `"'`)
		if item != "" {
		 out = append(out, item)
		}
	}
	return out
}
