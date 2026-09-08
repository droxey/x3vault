package config

import (
	"fmt"
	"strings"
)

// FormatWikiDirsSummary renders directory rules for CLI output.
func FormatWikiDirsSummary(w WikiDirs) string {
	var b strings.Builder
	b.WriteString("LLM Wiki directory rules (relative to source_root/wiki/):\n\n")
	b.WriteString("allowed_dirs:\n")
	if len(w.Allowed) == 0 {
		b.WriteString("  (none — all subdirectories except ignored)\n")
	} else {
		for _, d := range w.Allowed {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
	}
	b.WriteString("\nignored_dirs:\n")
	if len(w.Ignored) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, d := range w.Ignored {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
	}
	b.WriteString("\nRoot-level *.md files (index.md, log.md, overview.md, conventions.md, etc.) are always included.\n")
	b.WriteString("Defaults follow the Karpathy LLM Wiki layout (see github.com/microsoft/llmwiki).\n")
	return b.String()
}

func (w *WikiDirs) AddAllowed(dirs ...string) {
	for _, d := range dirs {
		d = cleanDirEntry(d)
		if d == "" {
			continue
		}
		if containsDir(w.Allowed, d) {
			continue
		}
		w.Allowed = append(w.Allowed, d)
	}
	w.Normalize()
}

func (w *WikiDirs) RemoveAllowed(dirs ...string) {
	w.Allowed = removeDirs(w.Allowed, dirs)
	w.Normalize()
}

func (w *WikiDirs) AddIgnored(dirs ...string) {
	for _, d := range dirs {
		d = cleanDirEntry(d)
		if d == "" {
			continue
		}
		if containsDir(w.Ignored, d) {
			continue
		}
		w.Ignored = append(w.Ignored, d)
	}
	w.Normalize()
}

func (w *WikiDirs) RemoveIgnored(dirs ...string) {
	w.Ignored = removeDirs(w.Ignored, dirs)
	w.Normalize()
}

func containsDir(list []string, dir string) bool {
	dir = cleanDirEntry(dir)
	for _, item := range list {
		if cleanDirEntry(item) == dir {
			return true
		}
	}
	return false
}

func removeDirs(list, remove []string) []string {
	rm := map[string]bool{}
	for _, d := range remove {
		rm[cleanDirEntry(d)] = true
	}
	var out []string
	for _, item := range list {
		if !rm[cleanDirEntry(item)] {
			out = append(out, item)
		}
	}
	return out
}
