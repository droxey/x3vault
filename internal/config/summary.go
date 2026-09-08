package config

import (
	"strings"
)

func FormatWikiDirsSummary(w WikiDirs) string {
	var b strings.Builder
	b.WriteString("Wiki directory rules (relative to wiki/):\n\n")
	b.WriteString("mode: ")
	b.WriteString(w.Mode)
	b.WriteString("\n\n")
	if w.Mode == WikiModeWhitelist {
		b.WriteString("allowed_dirs:\n")
		for _, d := range w.Allowed {
			b.WriteString("  - ")
			b.WriteString(d)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("all subdirectories are synced except ignored_dirs\n\n")
	}
	b.WriteString("ignored_dirs:\n")
	if len(w.Ignored) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, d := range w.Ignored {
			b.WriteString("  - ")
			b.WriteString(d)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nRoot-level *.md files are always included.\n")
	b.WriteString("Vault paths excluded from asset lookup: see sync.exclude_vault_paths in config.\n")
	if len(w.StandardDirs) > 0 {
		b.WriteString("Standard LLM Wiki folders: ")
		b.WriteString(strings.Join(w.StandardDirs, ", "))
		b.WriteString("\n")
	}
	return b.String()
}

func (w *WikiDirs) AddIgnored(dirs ...string) {
	for _, d := range dirs {
		d = cleanDirEntry(d)
		if d == "" || containsDir(w.Ignored, d) {
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

func (w *WikiDirs) AddAllowed(dirs ...string) {
	for _, d := range dirs {
		d = cleanDirEntry(d)
		if d == "" || containsDir(w.Allowed, d) {
			continue
		}
		w.Allowed = append(w.Allowed, d)
	}
	w.Mode = WikiModeWhitelist
	w.Normalize()
}

func (w *WikiDirs) RemoveAllowed(dirs ...string) {
	w.Allowed = removeDirs(w.Allowed, dirs)
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
