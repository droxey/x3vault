package markdown

import "strings"

// parseWikilink parses Obsidian wikilink inner text (without [[ ]]).
// Supports [[Page]], [[Page|Display]], [[Page#Heading]], [[Page#Heading|Display]].
func parseWikilink(inner string) (target, heading, label string) {
	inner = strings.TrimSpace(inner)
	labelPart := ""
	targetPart := inner
	if i := strings.Index(inner, "|"); i >= 0 {
		targetPart = strings.TrimSpace(inner[:i])
		labelPart = strings.TrimSpace(inner[i+1:])
	}
	if i := strings.Index(targetPart, "#"); i >= 0 {
		target = strings.TrimSpace(targetPart[:i])
		heading = strings.TrimSpace(targetPart[i+1:])
	} else {
		target = strings.TrimSpace(targetPart)
	}
	return target, heading, labelPart
}
