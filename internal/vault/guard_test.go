package vault

import (
	"path/filepath"
	"testing"

	"github.com/droxey/x3vault/internal/config"
)

func TestAssertBuildWritePathAllowsBuildRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "current", "wiki", "index.md")
	if err := AssertBuildWritePath(path, root); err != nil {
		t.Fatal(err)
	}
}

func TestAssertBuildWritePathRejectsVaultWiki(t *testing.T) {
	root := t.TempDir()
	wiki := filepath.Join(root, "wiki", "index.md")
	if err := AssertBuildWritePath(wiki, filepath.Join(root, config.ToolDirName, "build")); err == nil {
		t.Fatal("expected refusal to write into vault wiki")
	}
}

func TestIsObsidianManagedPath(t *testing.T) {
	root := t.TempDir()
	wiki := filepath.Join(root, "wiki", "index.md")
	obs := filepath.Join(root, ".obsidian", "app.json")
	build := filepath.Join(root, config.ToolDirName, "build", "current", "wiki", "index.md")

	if !IsObsidianManagedPath(wiki, root, "wiki") {
		t.Fatal("wiki path should be obsidian-managed")
	}
	if !IsObsidianManagedPath(obs, root, "wiki") {
		t.Fatal(".obsidian path should be obsidian-managed")
	}
	if IsObsidianManagedPath(build, root, "wiki") {
		t.Fatal("build output should not be obsidian-managed")
	}
}
