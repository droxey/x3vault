package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	code := runVersion(&out)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(out.String(), "x3vault") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunHelp(t *testing.T) {
	code := Run(context.Background(), []string{"x3vault", "help"})
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
}

func TestRunInitRequiresVault(t *testing.T) {
	code := Run(context.Background(), []string{"x3vault", "init"})
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code := Run(context.Background(), []string{"x3vault", "nope"})
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestRunBuildSmoke(t *testing.T) {
	vaultRoot := t.TempDir()
	wikiDir := filepath.Join(vaultRoot, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"x3vault", "init", "--vault", vaultRoot}); code != exitOK {
		t.Fatalf("init exit = %d", code)
	}
	var progress bytes.Buffer
	if code := runBuild(context.Background(), []string{"--vault", vaultRoot}, &progress); code != exitOK {
		t.Fatalf("build exit = %d, progress=%q", code, progress.String())
	}
	if !strings.Contains(progress.String(), "built generation") {
		t.Fatalf("expected build progress, got %q", progress.String())
	}
}
