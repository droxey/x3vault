package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/vault"
)

func TestRunCopiesReferencedAttachmentsToBuildAssets(t *testing.T) {
	vaultDir := t.TempDir()
	wiki := filepath.Join(vaultDir, "wiki", "entities")
	attach := filepath.Join(vaultDir, "raw", "assets")
	obsidian := filepath.Join(vaultDir, ".obsidian")
	for _, d := range []string{wiki, attach, obsidian} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(obsidian, "app.json"), []byte(`{"attachmentFolderPath":"raw/assets"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attach, "diagram.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attach, "paper.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := "See ![[diagram.png]] and [[paper.pdf]] and [doc](paper.pdf)\n"
	if err := os.WriteFile(filepath.Join(wiki, "note.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	buildRoot := filepath.Join(filepath.Dir(vaultDir), "ereader", "build")
	cfg := config.Default()
	cfg.VaultRoot = vaultDir
	cfg.BuildRoot = buildRoot
	if err := cfg.Resolve(config.ConfigPath(vaultDir)); err != nil {
		t.Fatal(err)
	}

	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, disc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Assets != 2 {
		t.Fatalf("assets = %d, want 2 (png embed + pdf wikilink/inline deduped)", res.Assets)
	}

	assetsDir := filepath.Join(buildRoot, "current", cfg.Build.AssetsRoot)
	var copied []string
	err = filepath.Walk(assetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			copied = append(copied, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk assets: %v", err)
	}
	if len(copied) != 2 {
		t.Fatalf("copied files = %v, want diagram.png and paper.pdf under %s", copied, assetsDir)
	}
}
