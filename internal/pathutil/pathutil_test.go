package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainedIn(t *testing.T) {
	parent := filepath.Join(string(filepath.Separator), "vault")
	child := filepath.Join(parent, "wiki", "note.md")
	if !ContainedIn(child, parent) {
		t.Fatalf("expected %q contained in %q", child, parent)
	}
	if ContainedIn(filepath.Join(string(filepath.Separator), "other"), parent) {
		t.Fatal("expected unrelated path not contained")
	}
	if !ContainedIn(parent, parent) {
		t.Fatal("expected path contained in itself")
	}
}

func TestContainedInFilesystemRoot(t *testing.T) {
	root := string(filepath.Separator)
	if !ContainedIn(filepath.Join(root, "vault", "note.md"), root) {
		t.Fatal("filesystem root must contain descendants")
	}
	if ContainedIn("../escape", ".") {
		t.Fatal("parent traversal contained")
	}
}

func TestCanonicalResolvesMissingDescendants(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skip(err)
	}
	got, err := Canonical(filepath.Join(alias, "missing", "output"))
	if err != nil {
		t.Fatal(err)
	}
	targetCanon, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(targetCanon, "missing", "output") {
		t.Fatalf("got %q", got)
	}
	if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, err := Canonical(filepath.Join(root, "dangling", "output")); err == nil {
		t.Fatal("accepted dangling ancestor")
	}
}
