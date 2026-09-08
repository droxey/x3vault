//go:build unix

package markdown

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
)

func TestNoteReaderRejectsFIFO(t *testing.T) {
	source, _ := noteFixture(t, nil)
	if err := syscall.Mkfifo(filepath.Join(source, "pipe.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := fixtureNoteReader(t, source)
	if _, err := reader.Read(context.Background(), "pipe.md"); err == nil {
		t.Fatal("FIFO note was accepted")
	}
}
