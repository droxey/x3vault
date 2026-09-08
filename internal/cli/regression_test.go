package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/droxey/x3vault/internal/config"
)

func cliVault(t *testing.T) string {
	t.Helper()
	v := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(v, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v, "wiki", "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLoadConfigRejectsInvalidExistingConfig(t *testing.T) {
	v := cliVault(t)
	p := config.ConfigPath(v)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("schema: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(v); err == nil {
		t.Fatal("invalid config silently replaced with defaults")
	}
}

func TestLoadConfigRelativeVaultWithoutConfig(t *testing.T) {
	v := cliVault(t)
	t.Chdir(filepath.Dir(v))
	cfg, err := loadConfig("vault")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultRoot != v {
		t.Fatalf("vault = %q, want %q", cfg.VaultRoot, v)
	}
}

func TestRunRejectsMalformedArgumentsBeforeBuilding(t *testing.T) {
	for _, args := range [][]string{
		{"--dry-rnu"}, {"--vault"}, {"unexpected"}, {"--json=false"},
		{"--vault", "--json"}, {"--vault", "one", "--vault", "two"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			v := cliVault(t)
			argv := append([]string{"x3vault", "build", "--vault", v}, args...)
			if code := Run(context.Background(), argv); code != exitUsage {
				t.Fatalf("code = %d, want usage error", code)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(v), "ereader")); !os.IsNotExist(err) {
				t.Fatalf("invalid arguments created build output: %v", err)
			}
		})
	}
}

func captureCLI(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	errout, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldout, olderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errout
	defer func() { os.Stdout, os.Stderr = oldout, olderr; out.Close(); errout.Close() }()
	code := fn()
	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := errout.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	e, err := io.ReadAll(errout)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(b), string(e)
}

func TestJSONFailureAlsoReportsStderr(t *testing.T) {
	v := cliVault(t)
	if err := os.RemoveAll(filepath.Join(v, "wiki")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := captureCLI(t, func() int {
		return Run(context.Background(), []string{"x3vault", "build", "--vault", v, "--json"})
	})
	if code != exitBuild || !json.Valid([]byte(stdout)) || !strings.Contains(stderr, "error:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestJSONUsageErrorIsMachineReadable(t *testing.T) {
	code, stdout, stderr := captureCLI(t, func() int {
		return Run(context.Background(), []string{"x3vault", "sync", "--json", "--dry-rnu"})
	})
	if code != exitUsage || !json.Valid([]byte(stdout)) || !strings.Contains(stderr, "--dry-rnu") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestHelpOutputFailureReturnsNonzero(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old }()
	if code := Run(context.Background(), []string{"x3vault", "build", "--help"}); code == exitOK {
		t.Fatal("help write failure returned success")
	}
}

func TestBuildHonorsCancelledContext(t *testing.T) {
	v := cliVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Run(ctx, []string{"x3vault", "build", "--vault", v}); code == exitOK {
		t.Fatal("cancelled build returned success")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(v), "ereader", "build", "current")); !os.IsNotExist(err) {
		t.Fatalf("cancelled build published output: %v", err)
	}
}

func TestVersionOutputFailureReturnsNonzero(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if code := runVersion(f); code == exitOK {
		t.Fatal("write failure returned success")
	}
}

func TestConfigDirectoryCommands(t *testing.T) {
	v := cliVault(t)
	if err := config.WriteDefault(config.ConfigPath(v)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		args             []string
		allowed, ignored bool
	}{
		{[]string{"ignore", "drafts"}, false, true},
		{[]string{"unignore", "drafts"}, false, false},
		{[]string{"allow", "notes"}, true, false},
		{[]string{"restore"}, false, false},
	} {
		args := append([]string{"x3vault", "config", "dirs"}, step.args...)
		args = append(args, "--vault", v)
		if code := Run(context.Background(), args); code != exitOK {
			t.Fatalf("%v exit %d", step.args, code)
		}
		cfg, err := config.Load(config.ConfigPath(v))
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.Wiki.Mode == config.WikiModeWhitelist; got != step.allowed {
			t.Fatalf("%v whitelist=%v", step.args, got)
		}
		if got := !cfg.Wiki.ShouldIncludeRelPath("drafts/note.md"); got != (step.ignored || step.allowed) {
			t.Fatalf("%v drafts ignored=%v", step.args, got)
		}
	}
}
