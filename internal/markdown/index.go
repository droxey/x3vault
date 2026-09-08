package markdown

import (
	"context"
	"path/filepath"
	"strings"
)

type NoteRef struct {
	RelPath string
	AbsPath string
}
type IndexResult struct{ Index map[string]string }

// BuildNoteIndex indexes names and metadata, then canonical note paths.
// Last-wins shorthand resolution is retained; aliases cannot shadow a real path.
// Cancellation stops indexing; callers must check ctx before using the result.
func BuildNoteIndex(ctx context.Context, notes []NoteRef, reader *NoteReader) IndexResult {
	if ctx == nil {
		ctx = context.Background()
	}
	res := IndexResult{Index: make(map[string]string)}
	add := func(key, rel string) {
		if key = normalizeIndexKey(key); key != "" {
			res.Index[key] = rel
		}
	}
	for _, n := range notes {
		if ctx.Err() != nil {
			return res
		}
		rel := filepath.ToSlash(n.RelPath)
		add(trimMarkdownExt(filepath.Base(rel)), rel)
		raw, err := reader.readReference(ctx, n.AbsPath, n.RelPath)
		if err != nil {
			continue
		} // Normalize reports unreadable and malformed notes.
		meta, _, err := readFrontmatter(raw)
		if err != nil {
			continue
		}
		add(meta.Title, rel)
		for _, alias := range meta.Aliases {
			add(alias, rel)
		}
	}
	for _, n := range notes {
		if ctx.Err() != nil {
			return res
		}
		rel := filepath.ToSlash(n.RelPath)
		add(rel, rel)
	}
	return res
}

func trimMarkdownExt(s string) string {
	if strings.EqualFold(filepath.Ext(s), ".md") {
		return s[:len(s)-3]
	}
	return s
}
func normalizeIndexKey(key string) string {
	return trimMarkdownExt(filepath.ToSlash(strings.TrimSpace(key)))
}
