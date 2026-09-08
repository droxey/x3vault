package markdown

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/droxey/x3vault/internal/config"
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
	}}, fixtureNoteReader(t, dir))

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
	}, fixtureNoteReader(t, dir))
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

func TestResolveAssetFromExcludedRawAttachmentFolder(t *testing.T) {
	dir := t.TempDir()
	wiki := filepath.Join(dir, "wiki", "entities")
	attach := filepath.Join(dir, "raw", "assets")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(attach, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attach, "paper.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	asset, err := resolveAsset("paper.pdf", "entities", NormalizeOpts{
		VaultRoot:              dir,
		SourceRoot:             filepath.Join(dir, "wiki"),
		SourceRel:              "wiki",
		AttachmentFolder:       attach,
		IsExcludedVaultPath:    cfg.IsExcludedVaultPath,
		ShouldIncludeSourceRel: cfg.Wiki.ShouldIncludeRelPath,
	})
	if err != nil {
		t.Fatalf("resolveAsset: %v", err)
	}
	if asset.SourceAbs != filepath.Join(attach, "paper.pdf") {
		t.Fatalf("SourceAbs = %q", asset.SourceAbs)
	}
}

func TestResolveAssetSkipsIgnoredDirectory(t *testing.T) {
	dir := t.TempDir()
	wiki := filepath.Join(dir, "wiki", "entities")
	ignored := filepath.Join(dir, "wiki", "script")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ignored, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignored, "secret.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	wikiDirs := config.DefaultWikiDirs()
	_, err := resolveAsset("script/secret.png", "entities", NormalizeOpts{
		VaultRoot:              dir,
		SourceRoot:             filepath.Join(dir, "wiki"),
		ShouldIncludeSourceRel: wikiDirs.ShouldIncludeRelPath,
	})
	if err == nil {
		t.Fatal("expected asset in ignored directory to be rejected")
	}
}

func TestNormalizeCopiesReferencedPDF(t *testing.T) {
	dir := t.TempDir()
	wiki := filepath.Join(dir, "wiki", "entities")
	attach := filepath.Join(dir, "attachments")
	out := filepath.Join(dir, "out", "assets")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(attach, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(attach, "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(wiki, "note.md")
	if err := os.WriteFile(notePath, []byte("See ![[paper.pdf]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	norm, err := Normalize(notePath, "entities/note.md", NormalizeOpts{
		VaultRoot:        dir,
		SourceRoot:       filepath.Join(dir, "wiki"),
		SourceRel:        "wiki",
		AssetsRoot:       "assets",
		NoteIndex:        map[string]string{},
		AttachmentFolder: attach,
		AssetOutDir:      out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(norm.Assets) != 1 {
		t.Fatalf("assets = %d, want 1", len(norm.Assets))
	}
	if norm.Assets[0].SourceAbs != pdfPath {
		t.Fatalf("SourceAbs = %q", norm.Assets[0].SourceAbs)
	}
	copied, err := os.ReadFile(filepath.Join(out, norm.Assets[0].HashPrefix, filepath.Base(norm.Assets[0].DeviceRel)))
	if err != nil {
		t.Fatalf("copied attachment missing: %v", err)
	}
	if string(copied) != "%PDF-1.4" {
		t.Fatalf("copied content = %q", copied)
	}
	src, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != "See ![[paper.pdf]]\n" {
		t.Fatal("source note was modified")
	}
}
