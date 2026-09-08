package markdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func fixtureNoteReader(t *testing.T, source string) *NoteReader {
	t.Helper()
	reader, err := OpenNoteReader(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	return reader
}

func TestNormalizeRejectsReplacedNote(t *testing.T) {
	for _, replacement := range []string{"outside symlink", "inside symlink", "directory"} {
		t.Run(replacement, func(t *testing.T) {
			source, opts := noteFixture(t, map[string]string{"note.md": "original", "other.md": "inside fixture"})
			note := filepath.Join(source, "note.md")
			if err := os.Remove(note); err != nil {
				t.Fatal(err)
			}
			if replacement == "directory" {
				if err := os.Mkdir(note, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(source, "other.md")
				if replacement == "outside symlink" {
					target = filepath.Join(t.TempDir(), "outside.md")
					if err := os.WriteFile(target, []byte("synthetic outside note"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, note); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if _, err := Normalize(note, "note.md", opts); err == nil {
				t.Fatal("replaced non-regular note was accepted")
			}
		})
	}
}

func TestNoteReaderRejectsSymlinkDirectory(t *testing.T) {
	source, _ := noteFixture(t, map[string]string{"notes/file.md": "note"})
	if err := os.Symlink(filepath.Join(source, "notes"), filepath.Join(source, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reader := fixtureNoteReader(t, source)
	if _, err := reader.Read(context.Background(), "alias/file.md"); err == nil {
		t.Fatal("symlink directory was accepted")
	}
}

func TestNoteReaderRejectsNonlocalPaths(t *testing.T) {
	source, _ := noteFixture(t, nil)
	reader := fixtureNoteReader(t, source)
	for _, rel := range []string{"../note.md", "notes/../note.md", "/note.md", `notes\note.md`, "./note.md"} {
		if _, err := reader.Read(context.Background(), rel); err == nil {
			t.Fatalf("nonlocal path accepted: %q", rel)
		}
	}
}

func TestIndexRejectsSymlinkedMetadata(t *testing.T) {
	source, _ := noteFixture(t, nil)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\ntitle: Foreign Title\naliases: [Foreign Alias]\n---\nfixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(source, "note.md")
	if err := os.Symlink(outside, note); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	index := BuildNoteIndex([]NoteRef{{RelPath: "note.md", AbsPath: note}}, fixtureNoteReader(t, source))
	if index.Index["Foreign Title"] != "" || index.Index["Foreign Alias"] != "" {
		t.Fatalf("foreign metadata was indexed: %v", index.Index)
	}
}

func TestNormalizeRequiresMatchingSourceReference(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"note.md": "inside note"})
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.NoteReader = fixtureNoteReader(t, source)
	if _, err := Normalize(outside, "note.md", opts); err == nil {
		t.Fatal("mismatched absolute source reference was accepted")
	}
}
