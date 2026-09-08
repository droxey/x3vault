package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestAttachmentFolderRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	obsidianDir := filepath.Join(dir, ".obsidian")
	if err := os.Mkdir(obsidianDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"/outside", "../outside", "safe/../outside", "C:/outside"} {
		if err := os.WriteFile(filepath.Join(obsidianDir, "app.json"), []byte(`{"attachmentFolderPath":"`+folder+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if got := AttachmentFolder(dir); got != "" {
			t.Errorf("accepted %q as %q", folder, got)
		}
		if got := ResolveAttachmentPath(dir, folder); got != "" {
			t.Errorf("resolved %q as %q", folder, got)
		}
	}
}

func TestAttachmentFolderRejectsSymlinkMetadata(t *testing.T) {
	for _, linkDir := range []bool{false, true} {
		t.Run(map[bool]string{false: "app file", true: "metadata directory"}[linkDir], func(t *testing.T) {
			vault := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "app.json")
			if err := os.WriteFile(target, []byte(`{"attachmentFolderPath":"external"}`), 0600); err != nil {
				t.Fatal(err)
			}
			metadata := filepath.Join(vault, ".obsidian")
			if linkDir {
				if err := os.Symlink(outside, metadata); err != nil {
					t.Skip(err)
				}
			} else {
				if err := os.Mkdir(metadata, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(metadata, "app.json")); err != nil {
					t.Skip(err)
				}
			}
			if got := AttachmentFolder(vault); got != "" {
				t.Fatalf("read symlinked metadata: %q", got)
			}
		})
	}
}

func TestAttachmentFolderAllowsVaultAlias(t *testing.T) {
	vault := t.TempDir()
	metadata := filepath.Join(vault, ".obsidian")
	if err := os.Mkdir(metadata, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "app.json"), []byte(`{"attachmentFolderPath":"raw/assets"}`), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "vault")
	if err := os.Symlink(vault, alias); err != nil {
		t.Skip(err)
	}
	if got := AttachmentFolder(alias); got != "raw/assets" {
		t.Fatalf("vault alias returned %q", got)
	}
}

func TestAttachmentFolderRejectsDirectoryMetadataFile(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".obsidian", "app.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := AttachmentFolder(vault); got != "" {
		t.Fatalf("read directory as metadata: %q", got)
	}
}

func TestAttachmentFolderRejectsPipeSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pipe descriptor fixture uses Linux /proc/self/fd")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// Buffer a small complete document and close the writer before calling the
	// loader, so even a regression that follows the pipe cannot block on EOF.
	if _, err := writer.Write([]byte(`{"attachmentFolderPath":"pipe-data"}`)); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	metadata := filepath.Join(vault, ".obsidian")
	if err := os.Mkdir(metadata, 0755); err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("/proc/self/fd/%d", reader.Fd())
	if _, err := os.Stat(target); err != nil {
		t.Skip("descriptor filesystem unavailable:", err)
	}
	if err := os.Symlink(target, filepath.Join(metadata, "app.json")); err != nil {
		t.Skip(err)
	}
	if got := AttachmentFolder(vault); got != "" {
		t.Fatalf("read pipe metadata: %q", got)
	}
}
