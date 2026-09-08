package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Obsidian-style patterns
var (
	reWikilink    = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	reEmbed       = regexp.MustCompile(`!\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	reComment     = regexp.MustCompile(`%%[\s\S]*?%%`)
	reFrontmatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
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
	VaultRoot             string
	SourceRoot            string
	SourceRel             string
	AssetsRoot            string
	NoteIndex             map[string]string
	AttachmentFolder      string
	IsExcludedVaultPath   func(string) bool
	ShouldIncludeSourceRel func(string) bool
	AssetOutDir           string
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
		if ext != "" && ext != ".md" {
			asset, err := resolveAsset(target, noteDir, opts)
			if err == nil {
				out.Assets = append(out.Assets, *asset)
				if alt == "" {
					alt = filepath.Base(target)
				}
				href := relPathFromNote(noteDir, asset.DeviceRel, opts.SourceRel)
				return imageMarkdown(alt, href, ext, out)
			}
			out.Warnings = append(out.Warnings, fmt.Sprintf("missing attachment %s: %v", target, err))
			if alt == "" {
				alt = target
			}
			if isImageExt(ext) {
				return imageMarkdown(alt, target, ext, out)
			}
			return fmt.Sprintf("[%s](%s)", alt, target)
		}

		resolved, ok := resolveNote(target, opts.NoteIndex)
		if !ok {
			out.Unresolved = append(out.Unresolved, target)
			return fmt.Sprintf("[embed: %s](%s)", target, target)
		}
		if opts.ShouldIncludeSourceRel != nil && !opts.ShouldIncludeSourceRel(resolved) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("embed target in ignored directory: %s", target))
			if alt == "" {
				alt = target
			}
			return fmt.Sprintf("[embed: %s](%s)", alt, target)
		}
		label := alt
		if label == "" {
			label = strings.TrimSuffix(filepath.Base(resolved), ".md")
		}
		href := relPathFromNote(noteDir, resolved, opts.SourceRel)
		return fmt.Sprintf("[%s](%s)", label, href)
	})

	text = reWikilink.ReplaceAllStringFunc(text, func(match string) string {
		sub := reWikilink.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		target, heading, label := parseWikilink(sub[1])

		if ext := strings.ToLower(filepath.Ext(target)); ext != "" && ext != ".md" {
			asset, err := resolveAsset(target, noteDir, opts)
			if err == nil {
				out.Assets = append(out.Assets, *asset)
				if label == "" {
					label = filepath.Base(target)
				}
				href := relPathFromNote(noteDir, asset.DeviceRel, opts.SourceRel)
				return fmtMarkdownLink(label, href)
			}
			out.Warnings = append(out.Warnings, fmt.Sprintf("missing attachment %s: %v", target, err))
			if label == "" {
				label = target
			}
			return fmt.Sprintf("[%s](%s)", label, target)
		}

		resolved, ok := resolveNote(target, opts.NoteIndex)
		if !ok {
			out.Unresolved = append(out.Unresolved, target)
			if label == "" {
				label = target
			}
			return fmt.Sprintf("[%s](%s)", label, target)
		}
		if opts.ShouldIncludeSourceRel != nil && !opts.ShouldIncludeSourceRel(resolved) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("wikilink target in ignored directory: %s", target))
			if label == "" {
				label = target
			}
			return fmt.Sprintf("[%s](%s)", label, target)
		}

		href := relPathFromNote(noteDir, resolved, opts.SourceRel)
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

	text = rewriteInlineRefs(text, noteDir, opts, out)
	out.Body = FormatForXTEReader(out.Title, out.Tags, text)
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

func assetCandidates(target, noteDir string, opts NormalizeOpts) []string {
	base := filepath.Base(target)
	assetsRoot := opts.AssetsRoot
	if assetsRoot == "" {
		assetsRoot = "assets"
	}
	var c []string
	add := func(p string) {
		if p == "" {
			return
		}
		if pathUnderDir(p, opts.AttachmentFolder) {
			c = append(c, p)
			return
		}
		if pathUnderDir(p, opts.SourceRoot) {
			if opts.ShouldIncludeSourceRel != nil {
				rel, err := filepath.Rel(opts.SourceRoot, p)
				if err == nil {
					rel = filepath.ToSlash(rel)
					if !opts.ShouldIncludeSourceRel(rel) {
						return
					}
				}
			}
		}
		if opts.IsExcludedVaultPath != nil {
			if rel, err := filepath.Rel(opts.VaultRoot, p); err == nil {
				if opts.IsExcludedVaultPath(filepath.ToSlash(rel)) {
					return
				}
			}
		}
		c = append(c, p)
	}
	if noteDir != "." && noteDir != "" {
		add(filepath.Join(opts.SourceRoot, noteDir, target))
	}
	add(filepath.Join(opts.SourceRoot, target))
	add(filepath.Join(opts.VaultRoot, target))
	if opts.AttachmentFolder != "" {
		add(filepath.Join(opts.AttachmentFolder, target))
		add(filepath.Join(opts.AttachmentFolder, base))
	}
	add(filepath.Join(opts.VaultRoot, "Attachments", base))
	add(filepath.Join(opts.VaultRoot, "assets", base))
	add(filepath.Join(opts.SourceRoot, assetsRoot, base))
	return c
}

func pathUnderDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dirAbs, pathAbs)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../")
}

func resolveAsset(target, noteDir string, opts NormalizeOpts) (*AssetRef, error) {
	candidates := assetCandidates(target, noteDir, opts)
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
	if opts.ShouldIncludeSourceRel != nil && pathUnderDir(found, opts.SourceRoot) {
		rel, err := filepath.Rel(opts.SourceRoot, found)
		if err == nil && !opts.ShouldIncludeSourceRel(filepath.ToSlash(rel)) {
			return nil, fmt.Errorf("ignored directory")
		}
	}

	data, err := os.ReadFile(found)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	prefix := hex.EncodeToString(sum[:])[:4]
	name := sanitizeName(filepath.Base(found))
	assetsRoot := opts.AssetsRoot
	if assetsRoot == "" {
		assetsRoot = "assets"
	}
	deviceRel := filepath.ToSlash(filepath.Join(assetsRoot, prefix, name))

	if opts.AssetOutDir != "" {
		dest := filepath.Join(opts.AssetOutDir, prefix, name)
		if err := copyFileToDir(found, dest); err != nil {
			return nil, err
		}
	}

	return &AssetRef{
		SourceAbs:  found,
		DeviceRel:  deviceRel,
		HashPrefix: prefix,
	}, nil
}

func copyFileToDir(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func relPathFromNote(noteDir, target, sourceRel string) string {
	if sourceRel == "" {
		sourceRel = "wiki"
	}
	from := filepath.Join(sourceRel, noteDir)
	to := filepath.ToSlash(target)
	srcPrefix := sourceRel + "/"
	// Note paths from the index are relative to source_root (e.g. entities/foo.md).
	if !strings.HasPrefix(to, srcPrefix) && strings.HasSuffix(to, ".md") {
		to = filepath.ToSlash(filepath.Join(sourceRel, target))
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
