package obsidian

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentFolderFromAppJSON(t *testing.T) {
	dir := t.TempDir()
	obsidianDir := filepath.Join(dir, ".obsidian")
	if err := os.MkdirAll(obsidianDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"attachmentFolderPath":"raw/assets"}`
	if err := os.WriteFile(filepath.Join(obsidianDir, "app.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := AttachmentFolder(dir)
	if got != "raw/assets" {
		t.Fatalf("AttachmentFolder() = %q, want raw/assets", got)
	}
	abs := ResolveAttachmentPath(dir, got)
	want := filepath.Join(dir, "raw", "assets")
	if abs != want {
		t.Fatalf("ResolveAttachmentPath() = %q, want %q", abs, want)
	}
}

func TestAttachmentFolderMissingUsesEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := AttachmentFolder(dir); got != "" {
		t.Fatalf("AttachmentFolder() = %q, want empty", got)
	}
}
