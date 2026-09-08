package pathutil

import (
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
