package markdown

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildNoteIndexAliases(t *testing.T) {
	dir := t.TempDir()
	notePath := filepath.Join(dir, "entities", "memex.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: Memex\naliases:\n  - Bush Memex\n  - personal wiki\n---\n\n# Memex\n"
	if err := os.WriteFile(notePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := BuildNoteIndex([]NoteRef{{
		RelPath: "entities/memex.md",
		AbsPath: notePath,
	}})

	if got := res.Index["Bush Memex"]; got != "entities/memex.md" {
		t.Fatalf("alias index = %q", got)
	}
	if got := res.Index["Memex"]; got != "entities/memex.md" {
		t.Fatalf("title index = %q", got)
	}
}

func TestBuildNoteIndexDuplicateBasenamePicksLast(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "entities", "foo.md")
	b := filepath.Join(dir, "concepts", "foo.md")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# foo"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := BuildNoteIndex([]NoteRef{
		{RelPath: "entities/foo.md", AbsPath: a},
		{RelPath: "concepts/foo.md", AbsPath: b},
	})
	if got := res.Index["foo"]; got != "concepts/foo.md" {
		t.Fatalf("expected last-wins basename, got %q", got)
	}
}

func TestResolveAssetFromObsidianAttachmentFolder(t *testing.T) {
	dir := t.TempDir()
	wiki := filepath.Join(dir, "wiki", "entities")
	attach := filepath.Join(dir, "raw", "assets")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(attach, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attach, "diagram.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	asset, err := resolveAsset("diagram.png", "entities", NormalizeOpts{
		VaultRoot:        dir,
		SourceRoot:       filepath.Join(dir, "wiki"),
		SourceRel:        "wiki",
		AttachmentFolder: attach,
	})
	if err != nil {
		t.Fatalf("resolveAsset: %v", err)
	}
	if asset.SourceAbs != filepath.Join(attach, "diagram.png") {
		t.Fatalf("SourceAbs = %q", asset.SourceAbs)
	}
}
