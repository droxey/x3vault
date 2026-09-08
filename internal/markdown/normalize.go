package markdown

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
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
	Tags       []string
	Assets     []AssetRef
	Unresolved []string
	Warnings   []string
}
type NormalizeOpts struct {
	Context                 context.Context
	NoteReader              *NoteReader
	VaultRoot               string
	SourceRoot              string
	SourceRel               string
	AssetsRoot              string
	NoteIndex               map[string]string
	AttachmentFolder        string
	AttachmentFolderForNote func(string) string
	IsExcludedVaultPath     func(string) bool
	ShouldIncludeSourceRel  func(string) bool
	AssetOutDir             string
	AssetCache              map[string]AssetRef
}

func Normalize(absPath, relPath string, opts NormalizeOpts) (*NormalizedNote, error) {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if err := opts.Context.Err(); err != nil {
		return nil, err
	}
	raw, err := readNote(opts.Context, absPath, relPath, opts)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", absPath, err)
	}
	meta, body, err := readFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	out := &NormalizedNote{RelPath: relPath, Title: meta.Title, Tags: meta.Tags}
	if out.Title == "" {
		out.Title = trimMarkdownExt(filepath.Base(relPath))
	}
	if opts.AttachmentFolderForNote != nil {
		opts.AttachmentFolder = opts.AttachmentFolderForNote(absPath)
	}
	if opts.AssetCache == nil {
		opts.AssetCache = make(map[string]AssetRef)
	}
	transform := &noteTransformer{opts: opts, out: out, noteDir: filepath.Dir(relPath), seen: make(map[string]bool)}
	text, err := rewriteMarkdown(string(body), transform)
	if err != nil {
		return nil, err
	}
	if err := opts.Context.Err(); err != nil {
		return nil, err
	}
	out.Body = formatDocument(out.Title, out.Tags, text)
	return out, nil
}

type noteTransformer struct {
	opts    NormalizeOpts
	out     *NormalizedNote
	noteDir string
	seen    map[string]bool
}

func (t *noteTransformer) addAsset(asset *AssetRef) {
	if !t.seen[asset.DeviceRel] {
		t.out.Assets = append(t.out.Assets, *asset)
		t.seen[asset.DeviceRel] = true
	}
}
func (t *noteTransformer) wiki(inner string, embed bool) (string, error) {
	if err := t.opts.Context.Err(); err != nil {
		return "", err
	}
	target, heading, label := parseWikilink(inner)
	if target == "" && heading != "" {
		if label == "" {
			label = heading
		}
		return fmtMarkdownLink(label, "#"+slugify(heading)), nil
	}
	resolved, ok := resolveNoteFrom(target, t.noteDir, t.opts.NoteIndex)
	if ok {
		if t.opts.ShouldIncludeSourceRel != nil && !t.opts.ShouldIncludeSourceRel(resolved) {
			t.out.Warnings = append(t.out.Warnings, "note target in ignored directory: "+target)
		} else {
			href := noteRelativePath(t.noteDir, resolved)
			if heading != "" {
				href += "#" + slugify(heading)
			}
			if label == "" {
				label = trimMarkdownExt(filepath.Base(resolved))
				if heading != "" {
					label = heading
				}
			}
			return fmtMarkdownLink(label, href), nil
		}
	} else if !strings.EqualFold(filepath.Ext(target), ".md") {
		asset, err := resolveAsset(target, t.noteDir, t.opts)
		if err == nil {
			t.addAsset(asset)
			href := assetRelativePath(t.noteDir, asset.DeviceRel, t.opts.SourceRel)
			if heading != "" {
				href += "#" + heading
			}
			if label == "" {
				label = filepath.Base(target)
			}
			if embed {
				return imageMarkdown(label, href, strings.ToLower(filepath.Ext(target)), t.out), nil
			}
			return fmtMarkdownLink(label, href), nil
		}
		if !errors.Is(err, errAssetUnavailable) {
			return "", fmt.Errorf("attachment %s: %w", target, err)
		}
		if filepath.Ext(target) != "" {
			t.out.Warnings = append(t.out.Warnings, fmt.Sprintf("missing attachment %s: %v", target, err))
		} else {
			t.out.Unresolved = append(t.out.Unresolved, target)
		}
	} else {
		t.out.Unresolved = append(t.out.Unresolved, target)
	}
	if label == "" {
		label = target
		if heading != "" {
			label = heading
		}
	}
	href := target
	if heading != "" {
		href += "#" + heading
	}
	return fmtMarkdownLink(label, href), nil
}

// inline returns an updated destination and whether the original image should
// become an ordinary link. URL fragments belong to the attachment and are kept.
func (t *noteTransformer) inline(target string, image bool) (href string, changed, demote bool, err error) {
	if err = t.opts.Context.Err(); err != nil {
		return
	}
	parsed, e := url.Parse(target)
	if e != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(target, "//") || parsed.Path == "" {
		return target, false, false, nil
	}
	if strings.EqualFold(filepath.Ext(parsed.Path), ".md") {
		return target, false, false, nil
	}
	asset, e := resolveAsset(parsed.Path, t.noteDir, t.opts)
	if e != nil {
		if !errors.Is(e, errAssetUnavailable) {
			return "", false, false, fmt.Errorf("attachment %s: %w", target, e)
		}
		t.out.Warnings = append(t.out.Warnings, fmt.Sprintf("missing inline attachment %s: %v", target, e))
		return target, false, false, nil
	}
	t.addAsset(asset)
	href = assetRelativePath(t.noteDir, asset.DeviceRel, t.opts.SourceRel)
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		href += target[i:]
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	demote = image && (!isImageExt(ext) || !XTEDevice.SupportsImage(ext))
	if demote && isImageExt(ext) {
		t.out.Warnings = append(t.out.Warnings, "image format "+ext+" may not display on XTE e-readers; linked instead")
	}
	return href, true, demote, nil
}
func resolveNote(target string, idx map[string]string) (string, bool) {
	rel, ok := idx[normalizeIndexKey(target)]
	return rel, ok
}
func resolveNoteFrom(target, noteDir string, idx map[string]string) (string, bool) {
	target = filepath.ToSlash(target)
	if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") {
		joined := filepath.ToSlash(filepath.Clean(filepath.Join(noteDir, filepath.FromSlash(target))))
		if joined == ".." || strings.HasPrefix(joined, "../") {
			return "", false
		}
		return resolveNote(joined, idx)
	}
	// A real local note takes precedence over a basename alias elsewhere.
	local := normalizeIndexKey(filepath.ToSlash(filepath.Join(noteDir, target)))
	if rel, ok := idx[local]; ok && normalizeIndexKey(rel) == local {
		return rel, true
	}
	return resolveNote(target, idx)
}
func noteRelativePath(noteDir, target string) string {
	rel, err := filepath.Rel(noteDir, filepath.FromSlash(target))
	if err != nil {
		return filepath.ToSlash(target)
	}
	return encodeLocalPath(filepath.ToSlash(rel))
}
func assetRelativePath(noteDir, target, sourceRel string) string {
	if sourceRel == "" {
		sourceRel = "wiki"
	}
	rel, err := filepath.Rel(filepath.Join(sourceRel, noteDir), filepath.FromSlash(target))
	if err != nil {
		return filepath.ToSlash(target)
	}
	return encodeLocalPath(filepath.ToSlash(rel))
}

func encodeLocalPath(s string) string {
	return strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F").Replace(s)
}
