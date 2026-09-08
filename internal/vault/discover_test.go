package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/droxey/x3vault/internal/config"
)

func TestDiscoverNotes(t *testing.T) {
	vaultDir := t.TempDir()
	wiki := filepath.Join(vaultDir, "wiki", "entities")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "wiki", "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, "wiki", "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "wiki", "script", "hidden.md"), []byte("# Hidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirs := config.DefaultWikiDirs()
	disc, err := Discover(vaultDir, "wiki", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(disc.Notes) != 2 {
		t.Fatalf("notes = %d, want 2 (index + entities/note; script/ ignored)", len(disc.Notes))
	}
}

func TestDiscoverRejectsMissingSource(t *testing.T) {
	vaultDir := t.TempDir()
	_, err := Discover(vaultDir, "wiki", config.DefaultWikiDirs())
	if err == nil {
		t.Fatal("expected error for missing wiki/")
	}
}

func TestDiscoverRejectsSourceOutsideVault(t *testing.T) {
	vaultDir := t.TempDir()
	outside := t.TempDir()
	wikiOutside := filepath.Join(outside, "wiki")
	if err := os.MkdirAll(wikiOutside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiOutside, "x.md"), []byte("# X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlink wiki -> outside/vault/wiki to simulate escape attempt
	link := filepath.Join(vaultDir, "wiki")
	if err := os.Symlink(wikiOutside, link); err != nil {
		t.Skip("symlink not supported:", err)
	}
	_, err := Discover(vaultDir, "wiki", config.DefaultWikiDirs())
	if err == nil {
		t.Fatal("expected error when source escapes vault via symlink")
	}
}

func TestDiscoverWhitelistMode(t *testing.T) {
	vaultDir := t.TempDir()
	for _, sub := range []string{"sources", "concepts", "other"} {
		dir := filepath.Join(vaultDir, "wiki", sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "n.md"), []byte("# N\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dirs := config.DefaultWikiDirs()
	dirs.Mode = config.WikiModeWhitelist
	dirs.Allowed = []string{"sources", "concepts"}
	dirs.Normalize()

	disc, err := Discover(vaultDir, "wiki", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(disc.Notes) != 2 {
		t.Fatalf("notes = %d, want 2 in whitelist mode", len(disc.Notes))
	}
}
