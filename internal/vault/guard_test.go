package vault

import (
	"os"
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
	if err := AssertBuildWritePath(wiki, filepath.Join(filepath.Dir(root), config.EreaderDirName, "build")); err == nil {
		t.Fatal("expected refusal to write into vault wiki")
	}
}

func TestAssertBuildWritePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "current")); err != nil {
		t.Skip(err)
	}
	if err := AssertBuildWritePath(filepath.Join(root, "current", "new", "note.md"), root); err == nil {
		t.Fatal("accepted symlink write escape")
	}
}
