package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Obsidian-style patterns
var (
	reWikilink    = regexp.MustCompile(`\[\[([^\]|#]+)(?:\|([^\]]+))?(?:#([^\]]+))?\]\]`)
	reEmbed       = regexp.MustCompile(`!\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	reComment     = regexp.MustCompile(`%%[\s\S]*?%%`)
	reFrontmatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
	reInlineTag   = regexp.MustCompile(`(?:^|\s)#([a-zA-Z0-9_/-]+)`)
)

type AssetRef struct {
	SourceAbs  string
	DeviceRel  string
	HashPrefix string
}

type NormalizedNote struct {
	RelPath    string
	Body       string
	Title      string
	Aliases    []string
	Tags       []string
	Assets     []AssetRef
	Unresolved []string
	Warnings   []string
}

type NormalizeOpts struct {
	VaultRoot   string
	SourceRoot  string
	NoteIndex   map[string]string
	AssetOutDir string
}

func BuildNoteIndex(notes []struct{ RelPath, AbsPath string }) map[string]string {
	idx := make(map[string]string)
	for _, n := range notes {
		rel := n.RelPath
		idx[rel] = rel
		noExt := strings.TrimSuffix(rel, ".md")
		idx[noExt] = rel
		base := strings.TrimSuffix(filepath.Base(rel), ".md")
		idx[base] = rel
	}
	return idx
}

func Normalize(absPath, relPath string, opts NormalizeOpts) (*NormalizedNote, error) {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", absPath, err)
	}
	text := string(raw)

	out := &NormalizedNote{
		RelPath: relPath,
	}

	text = reComment.ReplaceAllString(text, "")

	if m := reFrontmatter.FindStringSubmatch(text); m != nil {
		fm := m[1]
		out.Title = extractYAMLString(fm, "title")
		out.Aliases = extractYAMLList(fm, "aliases")
		out.Tags = extractYAMLList(fm, "tags")
		text = reFrontmatter.ReplaceAllString(text, "")
	}
	if out.Title == "" {
		out.Title = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}

	noteDir := filepath.Dir(relPath)

	text = reEmbed.ReplaceAllStringFunc(text, func(match string) string {
		sub := reEmbed.FindStringSubmatch(match)
		target := strings.TrimSpace(sub[1])
		alt := ""
		if len(sub) > 2 {
			alt = strings.TrimSpace(sub[2])
		}

		ext := strings.ToLower(filepath.Ext(target))
		if isImageExt(ext) {
			asset, err := resolveAsset(target, opts)
			if err != nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("missing asset %s: %v", target, err))
				if alt == "" {
					alt = target
				}
				return fmt.Sprintf("![%s](%s)", alt, target)
			}
			out.Assets = append(out.Assets, *asset)
			if alt == "" {
				alt = filepath.Base(target)
			}
			href := relPathFromNote(noteDir, asset.DeviceRel)
			return fmt.Sprintf("![%s](%s)", alt, href)
		}

		resolved, ok := resolveNote(target, opts.NoteIndex)
		if !ok {
			out.Unresolved = append(out.Unresolved, target)
			return fmt.Sprintf("[embed: %s](%s)", target, target)
		}
		label := alt
		if label == "" {
			label = strings.TrimSuffix(filepath.Base(resolved), ".md")
		}
		href := relPathFromNote(noteDir, resolved)
		return fmt.Sprintf("[%s](%s)", label, href)
	})

	text = reWikilink.ReplaceAllStringFunc(text, func(match string) string {
		sub := reWikilink.FindStringSubmatch(match)
		target := strings.TrimSpace(sub[1])
		label := ""
		heading := ""
		if len(sub) > 2 && sub[2] != "" {
			label = strings.TrimSpace(sub[2])
		}
		if len(sub) > 3 && sub[3] != "" {
			heading = strings.TrimSpace(sub[3])
		}

		resolved, ok := resolveNote(target, opts.NoteIndex)
		if !ok {
			out.Unresolved = append(out.Unresolved, target)
			if label == "" {
				label = target
			}
			return fmt.Sprintf("[%s](%s)", label, target)
		}

		href := relPathFromNote(noteDir, resolved)
		if heading != "" {
			href += "#" + slugify(heading)
		}
		if label == "" {
			label = strings.TrimSuffix(filepath.Base(resolved), ".md")
			if heading != "" {
				label = heading
			}
		}
		return fmt.Sprintf("[%s](%s)", label, href)
	})

	out.Body = strings.TrimSpace(text) + "\n"
	return out, nil
}

func resolveNote(target string, idx map[string]string) (string, bool) {
	target = strings.TrimSuffix(target, ".md")
	if rel, ok := idx[target]; ok {
		return rel, true
	}
	target = filepath.ToSlash(target)
	if rel, ok := idx[target]; ok {
		return rel, true
	}
	return "", false
}

func resolveAsset(target string, opts NormalizeOpts) (*AssetRef, error) {
	candidates := []string{
		filepath.Join(opts.VaultRoot, target),
		filepath.Join(opts.SourceRoot, target),
		filepath.Join(opts.VaultRoot, "Attachments", filepath.Base(target)),
		filepath.Join(opts.VaultRoot, "assets", filepath.Base(target)),
	}
	var found string
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			found = c
			break
		}
	}
	if found == "" {
		return nil, fmt.Errorf("not found")
	}

	data, err := os.ReadFile(found)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	prefix := hex.EncodeToString(sum[:])[:4]
	name := sanitizeName(filepath.Base(found))
	deviceRel := filepath.ToSlash(filepath.Join("assets", prefix, name))

	if opts.AssetOutDir != "" {
		dest := filepath.Join(opts.AssetOutDir, prefix, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return nil, err
		}
	}

	return &AssetRef{
		SourceAbs:  found,
		DeviceRel:  deviceRel,
		HashPrefix: prefix,
	}, nil
}

func relPathFromNote(noteDir, target string) string {
	from := filepath.Join("wiki", noteDir)
	to := target
	if !strings.HasPrefix(target, "assets/") {
		to = filepath.Join("wiki", target)
	}
	rel, err := filepath.Rel(from, to)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func isImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, " ", "_")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extractYAMLString(fm, key string) string {
	re := regexp.MustCompile(`(?m)^` + key + `:\s*["']?([^"'\n#]+)["']?`)
	if m := re.FindStringSubmatch(fm); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractYAMLList(fm, key string) []string {
	reInline := regexp.MustCompile(`(?m)^` + key + `:\s*\[([^\]]+)\]`)
	if m := reInline.FindStringSubmatch(fm); m != nil {
		parts := strings.Split(m[1], ",")
		var out []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, `"'`)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}
