package markdown

import (
	"strings"
	"testing"
)

func FuzzParseWikilink(f *testing.F) {
	f.Add("Page")
	f.Add("Page|Display")
	f.Add("Page#Heading")
	f.Add("Page#Heading|Display")
	f.Add("日本語#Résumé|A | B")
	f.Add(" #Local heading ")
	f.Fuzz(func(t *testing.T, inner string) {
		target, heading, label := parseWikilink(inner)
		for _, value := range []string{target, heading, label} {
			if strings.TrimSpace(value) != value {
				t.Fatalf("untrimmed parsed value: %q", value)
			}
		}
		canonical := target
		if heading != "" {
			canonical += "#" + heading
		}
		if label != "" {
			canonical += "|" + label
		}
		gotTarget, gotHeading, gotLabel := parseWikilink(canonical)
		if gotTarget != target || gotHeading != heading || gotLabel != label {
			t.Fatalf("parse did not round-trip: (%q,%q,%q) -> %q -> (%q,%q,%q)", target, heading, label, canonical, gotTarget, gotHeading, gotLabel)
		}
	})
}
