package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

type metadata struct {
	Title   string
	Aliases []string
	Tags    []string
}

// readFrontmatter shares YAML semantics between indexing and normalization.
// Only the leading metadata block is removed; the Markdown body is untouched.
func readFrontmatter(raw []byte) (metadata, []byte, error) {
	var meta metadata
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	first := bytes.IndexByte(raw, '\n')
	if first < 0 || strings.TrimSuffix(string(raw[:first]), "\r") != "---" {
		return meta, raw, nil
	}
	start := first + 1
	for pos := start; pos <= len(raw); {
		end := bytes.IndexByte(raw[pos:], '\n')
		next := len(raw)
		if end >= 0 {
			end += pos
			next = end + 1
		} else {
			end = len(raw)
		}
		line := strings.TrimSuffix(string(raw[pos:end]), "\r")
		if line == "---" || line == "..." {
			var doc yaml.Node
			if err := yaml.Unmarshal(raw[start:pos], &doc); err != nil {
				return meta, nil, fmt.Errorf("parse frontmatter: %w", err)
			}
			if len(doc.Content) != 0 {
				node := doc.Content[0]
				if node.Kind != yaml.MappingNode {
					return meta, nil, fmt.Errorf("frontmatter must be a YAML mapping")
				}
				// Decode the whole map too, so duplicate keys are rejected by yaml.v3.
				var values map[string]yaml.Node
				if err := node.Decode(&values); err != nil {
					return meta, nil, fmt.Errorf("parse frontmatter: %w", err)
				}
				if title, ok := values["title"]; ok {
					if title.Kind != yaml.ScalarNode {
						return meta, nil, fmt.Errorf("frontmatter title must be a scalar")
					}
					meta.Title = strings.TrimSpace(title.Value)
				}
				var err error
				if value, ok := values["aliases"]; ok {
					meta.Aliases, err = metadataList(value)
					if err != nil {
						return meta, nil, fmt.Errorf("frontmatter aliases: %w", err)
					}
				}
				if value, ok := values["tags"]; ok {
					meta.Tags, err = metadataList(value)
					if err != nil {
						return meta, nil, fmt.Errorf("frontmatter tags: %w", err)
					}
				}
			}
			return meta, raw[next:], nil
		}
		if next == len(raw) {
			break
		}
		pos = next
	}
	// An unclosed thematic break is Markdown, not a metadata block.
	return meta, raw, nil
}

func metadataList(node yaml.Node) ([]string, error) {
	if node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind == yaml.ScalarNode {
		value := strings.TrimSpace(node.Value)
		if value == "" {
			return nil, nil
		}
		return []string{value}, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("must be a scalar or sequence")
	}
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("list entries must be scalars")
		}
		if value := strings.TrimSpace(item.Value); value != "" {
			out = append(out, value)
		}
	}
	return out, nil
}
