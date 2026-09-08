package markdown

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reHighlight   = regexp.MustCompile(`==([^=\n]+)==`)
	reBlockID     = regexp.MustCompile(`(?:^|\s)\^([a-zA-Z0-9-]+)`)
	reInlineTagMD = regexp.MustCompile(`(?:^|[\s(])#([a-zA-Z0-9_/-]+)`)
	reMarkdownImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reMarkdownLink  = regexp.MustCompile(`(?m)\[([^\]]+)\]\(([^)]+)\)`)
)

// XTEDevice is the output profile for XTEINK e-readers running Witch Reader.
// All build output is normalized for on-device markdown viewing.
var XTEDevice = DeviceProfile{
	SupportedImageExts: map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
	},
}

type DeviceProfile struct {
	SupportedImageExts map[string]bool
}

func (p DeviceProfile) SupportsImage(ext string) bool {
	return p.SupportedImageExts[strings.ToLower(ext)]
}

// FormatForXTEReader produces markdown meant to be read on an XTE e-reader screen.
func FormatForXTEReader(title string, tags []string, body string) string {
	body = strings.TrimSpace(body)
	body = reHighlight.ReplaceAllString(body, "**$1**")
	body = reBlockID.ReplaceAllString(body, "")
	body = reHTMLComment.ReplaceAllString(body, "")
	body = reInlineTagMD.ReplaceAllString(body, " ")
	body = collapseBlankLines(body)

	if title != "" {
		body = prependTitleHeading(title, body)
	}
	if len(tags) > 0 {
		body += "\n\n---\n\n*Tags: " + strings.Join(tags, ", ") + "*"
	}
	if body == "" {
		return "\n"
	}
	return body + "\n"
}

func prependTitleHeading(title, body string) string {
	h1 := "# " + title
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return h1
	}
	firstLine := strings.TrimSpace(strings.SplitN(trimmed, "\n", 2)[0])
	if firstLine == h1 {
		return trimmed
	}
	return h1 + "\n\n" + trimmed
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isRemoteRef(target string) bool {
	lower := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "data:")
}

func stripAnchor(target string) string {
	if i := strings.Index(target, "#"); i >= 0 {
		return target[:i]
	}
	return target
}

func fragment(target string) string {
	if i := strings.Index(target, "#"); i >= 0 {
		return target[i+1:]
	}
	return ""
}

func imageMarkdown(label, href, ext string, out *NormalizedNote) string {
	if isImageExt(ext) && XTEDevice.SupportsImage(ext) {
		return fmtMarkdownImage(label, href)
	}
	if isImageExt(ext) {
		out.Warnings = append(out.Warnings,
			"image format "+ext+" may not display on XTE e-readers; linked instead")
	}
	return fmtMarkdownLink(label, href)
}

func fmtMarkdownImage(alt, href string) string {
	return "![" + alt + "](" + href + ")"
}

func fmtMarkdownLink(label, href string) string {
	return "[" + label + "](" + href + ")"
}

func rewriteInlineRefs(text, noteDir string, opts NormalizeOpts, out *NormalizedNote) string {
	text = reMarkdownImage.ReplaceAllStringFunc(text, func(match string) string {
		sub := reMarkdownImage.FindStringSubmatch(match)
		alt := sub[1]
		target := strings.TrimSpace(sub[2])
		return rewriteInlineTarget(match, alt, target, noteDir, opts, out, true)
	})
	text = reMarkdownLink.ReplaceAllStringFunc(text, func(match string) string {
		sub := reMarkdownLink.FindStringSubmatch(match)
		label := sub[1]
		target := strings.TrimSpace(sub[2])
		ext := strings.ToLower(filepath.Ext(stripAnchor(target)))
		if ext == "" || ext == ".md" || isRemoteRef(target) {
			return match
		}
		return rewriteInlineTarget(match, label, target, noteDir, opts, out, false)
	})
	return text
}

func rewriteInlineTarget(fallback, label, target, noteDir string, opts NormalizeOpts, out *NormalizedNote, asImage bool) string {
	if isRemoteRef(target) {
		return fallback
	}
	assetsRoot := opts.AssetsRoot
	if assetsRoot == "" {
		assetsRoot = "assets"
	}
	clean := filepath.ToSlash(stripAnchor(target))
	if strings.HasPrefix(clean, assetsRoot+"/") || strings.Contains(clean, "/"+assetsRoot+"/") {
		return fallback
	}
	ext := strings.ToLower(filepath.Ext(stripAnchor(target)))
	if ext == "" || ext == ".md" {
		return fallback
	}
	asset, err := resolveAsset(stripAnchor(target), noteDir, opts)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("missing inline attachment %s: %v", target, err))
		return fallback
	}
	out.Assets = append(out.Assets, *asset)
	href := relPathFromNote(noteDir, asset.DeviceRel, opts.SourceRel)
	if frag := fragment(target); frag != "" {
		href += "#" + slugify(frag)
	}
	if asImage && isImageExt(ext) {
		return imageMarkdown(label, href, ext, out)
	}
	return fmtMarkdownLink(label, href)
}
