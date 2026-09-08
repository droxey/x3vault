package markdown

import "testing"

func FuzzParseWikilink(f *testing.F) {
	f.Add("Page")
	f.Add("Page|Display")
	f.Add("Page#Heading")
	f.Add("Page#Heading|Display")
	f.Fuzz(func(t *testing.T, inner string) {
		target, heading, label := parseWikilink(inner)
		if target != "" && heading != "" && label != "" {
			_ = target + heading + label
		}
	})
}
