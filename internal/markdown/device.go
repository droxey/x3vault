package markdown

import (
	"strings"
	"unicode"
)

// XTEDevice lists the formats emitted as images. Other attachments remain links.
var XTEDevice = DeviceProfile{SupportedImageExts: map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}}

type DeviceProfile struct{ SupportedImageExts map[string]bool }

func (p DeviceProfile) SupportsImage(ext string) bool {
	return p.SupportedImageExts[strings.ToLower(ext)]
}

func FormatForXTEReader(title string, tags []string, body string) string {
	body, _ = rewriteMarkdown(body, nil)
	return formatDocument(title, tags, body)
}
func formatDocument(title string, tags []string, body string) string {
	// Do not TrimSpace: four leading spaces make an indented code block, and
	// trailing spaces can be a Markdown hard break.
	body = strings.Trim(body, "\r\n")
	if title != "" {
		body = prependTitleHeading(title, body)
	}
	if len(tags) > 0 {
		body += "\n\n---\n\n*Tags: " + escapeLabel(strings.Join(tags, ", ")) + "*"
	}
	return body + "\n"
}
func prependTitleHeading(title, body string) string {
	h1 := "# " + escapeLabel(strings.ReplaceAll(strings.ReplaceAll(title, "\r", " "), "\n", " "))
	if body == "" {
		return h1
	}
	first := strings.SplitN(body, "\n", 2)[0]
	if heading, ok := parseATXHeading(first); ok && strings.EqualFold(heading, title) {
		return body
	}
	return h1 + "\n\n" + body
}
func parseATXHeading(line string) (string, bool) {
	line = strings.TrimLeft(line, " ")
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i < 1 || i > 6 || i >= len(line) || (line[i] != ' ' && line[i] != '\t') {
		return "", false
	}
	text := strings.TrimSpace(line[i:])
	// Optional closing hashes must be separated by whitespace.
	end := strings.TrimRight(text, "#")
	if len(end) < len(text) && len(end) > 0 && (end[len(end)-1] == ' ' || end[len(end)-1] == '\t') {
		text = strings.TrimSpace(end)
	}
	return text, text != ""
}
func imageMarkdown(label, href, ext string, out *NormalizedNote) string {
	if isImageExt(ext) && XTEDevice.SupportsImage(ext) {
		return fmtMarkdownImage(label, href)
	}
	if isImageExt(ext) {
		out.Warnings = append(out.Warnings, "image format "+ext+" may not display on XTE e-readers; linked instead")
	}
	return fmtMarkdownLink(label, href)
}
func fmtMarkdownImage(label, href string) string { return "!" + fmtMarkdownLink(label, href) }
func fmtMarkdownLink(label, href string) string {
	return "[" + escapeLabel(label) + "](" + escapeDestination(href) + ")"
}
func escapeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\[]*_`", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
func escapeDestination(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ':
			b.WriteString("%20")
		case '\n':
			b.WriteString("%0A")
		case '\r':
			b.WriteString("%0D")
		case '\t':
			b.WriteString("%09")
		case '<':
			b.WriteString("%3C")
		case '>':
			b.WriteString("%3E")
		case '\\', '(', ')':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	return b.String()
}
func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}
