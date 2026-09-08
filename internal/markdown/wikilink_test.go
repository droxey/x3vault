package markdown

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParseWikilink(t *testing.T) {
	cases := []struct {
		in                     string
		target, heading, label string
	}{
		{"Page", "Page", "", ""},
		{"Page|Display", "Page", "", "Display"},
		{"Page#Heading", "Page", "Heading", ""},
		{"Page#Heading|Display", "Page", "Heading", "Display"},
		{"folder/note", "folder/note", "", ""},
		{"folder/note#Section|Alias", "folder/note", "Section", "Alias"},
	}
	for _, tc := range cases {
		target, heading, label := parseWikilink(tc.in)
		if target != tc.target || heading != tc.heading || label != tc.label {
			t.Fatalf("parseWikilink(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.in, target, heading, label, tc.target, tc.heading, tc.label)
		}
	}
}

func TestNormalizeWikilinkHeadingAlias(t *testing.T) {
	dir := t.TempDir()
	wiki := t.TempDir()
	notePath := wiki + "/entities/page.md"
	if err := os.MkdirAll(wiki+"/entities", 0o755); err != nil {
		t.Fatal(err)
	}
	targetPath := wiki + "/concepts/target.md"
	if err := os.MkdirAll(wiki+"/concepts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("# Target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte("See [[target#My Heading|Custom Label]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := BuildNoteIndex(context.Background(), []NoteRef{
		{RelPath: "entities/page.md", AbsPath: notePath},
		{RelPath: "concepts/target.md", AbsPath: targetPath},
	}, fixtureNoteReader(t, wiki))
	norm, err := Normalize(notePath, "entities/page.md", NormalizeOpts{
		VaultRoot:  dir,
		SourceRoot: wiki,
		SourceRel:  "wiki",
		NoteIndex:  idx.Index,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "[Custom Label](../concepts/target.md#my-heading)"
	if !strings.Contains(norm.Body, want) {
		t.Fatalf("body = %q, want substring %q", norm.Body, want)
	}
}
