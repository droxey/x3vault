package markdown

import (
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type sourceSpan struct{ start, end int }
type sourceEdit struct {
	sourceSpan
	value string
}

var kindObsidian = ast.NewNodeKind("Obsidian")

type obsidianNode struct {
	ast.BaseInline
	span  sourceSpan
	kind  string
	value string
	embed bool
}

func (n *obsidianNode) Kind() ast.NodeKind            { return kindObsidian }
func (n *obsidianNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type obsidianParser struct{}

func (p *obsidianParser) Trigger() []byte { return []byte{'[', '!', '%', '=', '^'} }
func (p *obsidianParser) Parse(parent ast.Node, r text.Reader, pc parser.Context) ast.Node {
	line, seg := r.PeekLine()
	if len(line) == 0 {
		return nil
	}
	makeNode := func(size int, kind, value string, embed bool) ast.Node {
		r.Advance(size)
		return &obsidianNode{span: sourceSpan{seg.Start, seg.Start + size}, kind: kind, value: value, embed: embed}
	}
	// Goldmark handles escapes and code contexts before dispatching these parsers.
	prefix := 0
	embed := false
	if bytes.HasPrefix(line, []byte("![[")) {
		prefix = 3
		embed = true
	} else if bytes.HasPrefix(line, []byte("[[")) {
		prefix = 2
	}
	if prefix > 0 {
		if end := bytes.Index(line[prefix:], []byte("]]")); end >= 0 {
			end += prefix
			if end > prefix {
				return makeNode(end+2, "wiki", string(line[prefix:end]), embed)
			}
		}
		return nil
	}
	if bytes.HasPrefix(line, []byte("%%")) {
		// A comment may span lines, including Markdown syntax which is literal
		// inside the comment. Reader.Value retains source offsets across blocks.
		src := r.Source()
		if end := bytes.Index(src[seg.Start+2:], []byte("%%")); end >= 0 {
			stop := seg.Start + 2 + end + 2
			r.Advance(stop - seg.Start)
			return &obsidianNode{span: sourceSpan{seg.Start, stop}, kind: "comment"}
		}
	}
	if bytes.HasPrefix(line, []byte("==")) {
		if end := highlightEnd(line); end > 2 {
			return makeNode(end+2, "highlight", string(line[2:end]), false)
		}
	}
	if line[0] == '^' && unicode.IsSpace(r.PrecendingCharacter()) {
		end := 1
		letter := false
		for end < len(line) {
			c := line[end]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				letter = true
			} else if !(c >= '0' && c <= '9' || c == '-') {
				break
			}
			end++
		}
		if letter && end > 1 && len(bytes.TrimSpace(line[end:])) == 0 {
			return makeNode(end, "remove", "", false)
		}
	}
	return nil
}

// Goldmark's link parser resolves all CommonMark destination/title/reference
// forms. This wrapper records source bounds so edits preserve the rest of the
// document instead of serializing a lossy Markdown approximation.
type spanLinkParser struct {
	delegate parser.InlineParser
	starts   map[ast.Node]int
}

func (p *spanLinkParser) Trigger() []byte { return p.delegate.Trigger() }
func (p *spanLinkParser) Parse(parent ast.Node, r text.Reader, pc parser.Context) ast.Node {
	_, before := r.Position()
	start := -1
	if r.Peek() == ']' {
		for child := parent.LastChild(); child != nil; child = child.PreviousSibling() {
			if s, ok := p.starts[child]; ok {
				start = s
				delete(p.starts, child)
				break
			}
		}
	}
	node := p.delegate.Parse(parent, r, pc)
	if node == nil {
		return nil
	}
	switch node.(type) {
	case *ast.Link, *ast.Image:
		if start >= 0 {
			_, end := r.Position()
			node.SetAttributeString("x3span", sourceSpan{start, end.Start})
		}
	default:
		p.starts[node] = before.Start
	}
	return node
}

func rewriteMarkdown(input string, transform *noteTransformer) (string, error) {
	source := []byte(input)
	inline := parser.DefaultInlineParsers()
	for i, item := range inline {
		if item.Value == parser.NewLinkParser() {
			inline[i].Value = &spanLinkParser{delegate: parser.NewLinkParser(), starts: make(map[ast.Node]int)}
		}
	}
	inline = append(inline, util.Prioritized(&obsidianParser{}, 150))
	p := parser.NewParser(parser.WithBlockParsers(parser.DefaultBlockParsers()...), parser.WithInlineParsers(inline...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...))
	doc := p.Parse(text.NewReader(source))
	var hidden []sourceSpan
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if n, ok := node.(*obsidianNode); ok && n.kind == "comment" {
				hidden = append(hidden, n.span)
			}
		}
		return ast.WalkContinue, nil
	})
	isHidden := func(span sourceSpan) bool {
		for _, h := range hidden {
			if span.start >= h.start && span.start < h.end {
				return true
			}
		}
		return false
	}
	var edits []sourceEdit
	add := func(span sourceSpan, value string) {
		if span.start >= 0 && span.end >= span.start && span.end <= len(source) {
			edits = append(edits, sourceEdit{span, value})
		}
	}
	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n, ok := node.(*obsidianNode); ok && n.kind != "comment" && isHidden(n.span) {
			return ast.WalkSkipChildren, nil
		}
		if span, ok := node.AttributeString("x3span"); ok && isHidden(span.(sourceSpan)) {
			return ast.WalkSkipChildren, nil
		}
		switch n := node.(type) {
		case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.CodeSpan, *ast.AutoLink:
			return ast.WalkSkipChildren, nil
		case *obsidianNode:
			switch n.kind {
			case "remove", "comment":
				add(n.span, "")
			case "highlight":
				value, err := rewriteMarkdown(n.value, transform)
				if err != nil {
					return ast.WalkStop, err
				}
				add(n.span, "**"+value+"**")
			case "wiki":
				if transform != nil {
					value, err := transform.wiki(n.value, n.embed)
					if err != nil {
						return ast.WalkStop, err
					}
					add(n.span, value)
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			if transform != nil {
				if err := rewriteParsedLink(n, n.Destination, n.Title, false, source, transform, add); err != nil {
					return ast.WalkStop, err
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			if transform != nil {
				if err := rewriteParsedLink(n, n.Destination, n.Title, true, source, transform, add); err != nil {
					return ast.WalkStop, err
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			if n.Segments.Len() > 0 {
				start := n.Segments.At(0).Start
				end := n.Segments.At(n.Segments.Len() - 1).Stop
				if bytes.HasPrefix(source[start:end], []byte("<!--")) {
					add(sourceSpan{start, end}, "")
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock:
			if n.HTMLBlockType == ast.HTMLBlockType2 && n.Lines().Len() > 0 {
				start := n.Lines().At(0).Start
				end := n.Lines().At(n.Lines().Len() - 1).Stop
				if n.HasClosure() {
					end = n.ClosureLine.Stop
				}
				body := source[start:end]
				if open := bytes.Index(body, []byte("<!--")); open >= 0 {
					if close := bytes.Index(body[open+4:], []byte("-->")); close >= 0 {
						add(sourceSpan{start + open, start + open + 4 + close + 3}, "")
					}
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end > edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var out strings.Builder
	offset := 0
	for _, edit := range edits {
		if edit.start < offset {
			continue
		} // An enclosing comment owns its whole span.
		out.Write(source[offset:edit.start])
		out.WriteString(edit.value)
		offset = edit.end
	}
	out.Write(source[offset:])
	return out.String(), nil
}
func rewriteParsedLink(node ast.Node, destination, title []byte, image bool, source []byte, transform *noteTransformer, add func(sourceSpan, string)) error {
	value, ok := node.AttributeString("x3span")
	if !ok {
		return nil
	}
	span := value.(sourceSpan)
	if span.start < 0 || span.end > len(source) || span.end <= span.start {
		return fmt.Errorf("invalid parsed link source span")
	}
	target := html.UnescapeString(string(util.UnescapePunctuations(destination)))
	href, changed, demote, err := transform.inline(target, image)
	if err != nil {
		return err
	}
	original := string(source[span.start:span.end])
	// Keep the authored label (including emphasis/code), replacing only its link.
	open := strings.IndexByte(original, '[')
	close := matchingLabelEnd(original, open)
	if close < 0 {
		return fmt.Errorf("parsed link label bounds missing")
	}
	label := original[open+1 : close]
	var labelEdits []sourceEdit
	err = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || child == node {
			return ast.WalkContinue, nil
		}
		if nested, ok := child.(*ast.Image); ok {
			err := rewriteParsedLink(nested, nested.Destination, nested.Title, true, source, transform, func(span sourceSpan, value string) { labelEdits = append(labelEdits, sourceEdit{span, value}) })
			return ast.WalkSkipChildren, err
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return err
	}
	if len(labelEdits) > 0 {
		sort.Slice(labelEdits, func(i, j int) bool { return labelEdits[i].start < labelEdits[j].start })
		var rewritten strings.Builder
		offset := span.start + open + 1
		for _, edit := range labelEdits {
			if edit.start < offset || edit.end > span.start+close {
				continue
			}
			rewritten.Write(source[offset:edit.start])
			rewritten.WriteString(edit.value)
			offset = edit.end
		}
		rewritten.Write(source[offset : span.start+close])
		label = rewritten.String()
	}
	if !changed {
		if len(labelEdits) > 0 {
			add(span, original[:open+1]+label+original[close:])
		}
		return nil
	}
	result := "[" + label + "](" + escapeDestination(href)
	if len(title) > 0 {
		resolved := html.UnescapeString(string(util.UnescapePunctuations(title)))
		result += " \"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(resolved) + "\""
	}
	result += ")"
	if image && !demote {
		result = "!" + result
	}
	add(span, result)
	return nil
}
func matchingLabelEnd(s string, open int) int {
	if open < 0 {
		return -1
	}
	depth := 1
	for i := open + 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '`' {
			count := 1
			for i+count < len(s) && s[i+count] == '`' {
				count++
			}
			marker := strings.Repeat("`", count)
			if end := strings.Index(s[i+count:], marker); end >= 0 {
				i += count + end + count - 1
				continue
			}
		}
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// Find the closing highlight delimiter without interpreting delimiters inside
// an inline code span or a link label/destination as highlight syntax.
func highlightEnd(line []byte) int {
	for i := 2; i < len(line)-1; i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '`' {
			count := 1
			for i+count < len(line) && line[i+count] == '`' {
				count++
			}
			marker := bytes.Repeat([]byte{'`'}, count)
			if end := bytes.Index(line[i+count:], marker); end >= 0 {
				i += count + end + count - 1
				continue
			}
		}
		if line[i] == '[' {
			if end := matchingLabelEnd(string(line), i); end >= 0 {
				i = end
				if i+1 < len(line) && line[i+1] == '(' {
					depth := 1
					i += 2
					for ; i < len(line) && depth > 0; i++ {
						if line[i] == '\\' {
							i++
							continue
						}
						if line[i] == '(' {
							depth++
						}
						if line[i] == ')' {
							depth--
						}
					}
					i--
				}
				continue
			}
		}
		if line[i] == '=' && line[i+1] == '=' {
			return i
		}
		if line[i] == '\n' || line[i] == '\r' {
			return -1
		}
	}
	return -1
}
