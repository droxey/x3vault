package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	res, err := Run(cfg, disc, RunOptions{})
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

func TestRunBacksUpPreviousCurrentBuild(t *testing.T) {
	buildRoot := t.TempDir()
	current := filepath.Join(buildRoot, buildCurrentDir)
	backup := filepath.Join(buildRoot, buildBackupDir)
	if err := os.MkdirAll(filepath.Join(current, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "build.manifest"), []byte("generation: g-old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(backup, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "build.manifest"), []byte("generation: g-older\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	vaultDir := t.TempDir()
	wiki := filepath.Join(vaultDir, "wiki")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if _, err := Run(cfg, disc, RunOptions{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(backup, "build.manifest"))
	if err != nil {
		t.Fatalf("backup manifest missing: %v", err)
	}
	if string(data) != "generation: g-old\n" {
		t.Fatalf("backup manifest = %q", data)
	}
	if _, err := os.Stat(filepath.Join(backup, "wiki")); err != nil {
		t.Fatalf("backup wiki dir missing: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current build missing: %v", err)
	}
}

func TestBackupCurrentBuildReturnsBackedUp(t *testing.T) {
	buildRoot := t.TempDir()
	ok, err := backupCurrentBuild(buildRoot)
	if err != nil || ok {
		t.Fatalf("backupCurrentBuild(empty) = (%v, %v), want (false, nil)", ok, err)
	}

	current := filepath.Join(buildRoot, buildCurrentDir)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, err = backupCurrentBuild(buildRoot)
	if err != nil || !ok {
		t.Fatalf("backupCurrentBuild(with current) = (%v, %v), want (true, nil)", ok, err)
	}
	if _, err := os.Stat(filepath.Join(buildRoot, buildBackupDir)); err != nil {
		t.Fatalf("backup dir missing: %v", err)
	}
}

func TestRestoreBackupBuild(t *testing.T) {
	buildRoot := t.TempDir()
	backup := filepath.Join(buildRoot, buildBackupDir)
	if err := os.MkdirAll(filepath.Join(backup, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "build.manifest"), []byte("generation: g-old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreBackupBuild(buildRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(buildRoot, buildCurrentDir, "build.manifest")); err != nil {
		t.Fatalf("current not restored: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup should be gone after restore, err=%v", err)
	}
}

func buildFixture(t *testing.T) (*config.Config, *vault.Discovery) {
	t.Helper()
	base := t.TempDir()
	cfg := config.Default()
	cfg.VaultRoot = filepath.Join(base, "vault")
	cfg.BuildRoot = filepath.Join(base, "build")
	writeBuildFixture(t, filepath.Join(cfg.VaultRoot, "wiki", "index.md"), "# Index\n")
	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, disc
}

func writeBuildFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireBuildFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("read %s = %q, %v; want %q", path, got, err, want)
	}
}

type buildProgressFunc func([]byte) (int, error)

func (f buildProgressFunc) Write(p []byte) (int, error) { return f(p) }

func TestRunKeepsCurrentAndBackupWhenNormalizationFails(t *testing.T) {
	cfg, disc := buildFixture(t)
	current := filepath.Join(cfg.BuildRoot, "current", "build.manifest")
	backup := filepath.Join(cfg.BuildRoot, "backup", "build.manifest")
	writeBuildFixture(t, current, "current")
	writeBuildFixture(t, backup, "backup")
	if err := os.Remove(disc.Notes[0].AbsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(cfg, disc, RunOptions{}); err == nil {
		t.Fatal("missing source note should fail the build")
	}
	requireBuildFile(t, current, "current")
	requireBuildFile(t, backup, "backup")
	if _, err := os.Stat(filepath.Join(cfg.BuildRoot, "staging")); !os.IsNotExist(err) {
		t.Fatalf("failed staging remains: %v", err)
	}
}

func TestRunRejectsInvalidPathsBeforeMutation(t *testing.T) {
	for _, mode := range []string{"ignored traversal", "vault build root", "symlink build root", "symlink staging", "source escape"} {
		t.Run(mode, func(t *testing.T) {
			cfg, disc := buildFixture(t)
			current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
			writeBuildFixture(t, current, "current")
			sentinel := filepath.Join(cfg.VaultRoot, "sentinel")
			writeBuildFixture(t, sentinel, "vault")
			switch mode {
			case "ignored traversal":
				cfg.Wiki.Ignored = []string{"safe/../../../../victim"}
			case "vault build root":
				cfg.BuildRoot = cfg.VaultRoot
			case "symlink build root":
				cfg.BuildRoot = filepath.Join(filepath.Dir(cfg.VaultRoot), "alias")
				if err := os.Symlink(cfg.VaultRoot, cfg.BuildRoot); err != nil {
					t.Skip(err)
				}
			case "symlink staging":
				if err := os.Symlink(cfg.VaultRoot, filepath.Join(cfg.BuildRoot, "staging")); err != nil {
					t.Skip(err)
				}
			case "source escape":
				cfg.SourceRoot = "../victim"
			}
			if _, err := Run(cfg, disc, RunOptions{}); err == nil {
				t.Fatal("unsafe config should fail before writing")
			}
			requireBuildFile(t, sentinel, "vault")
			requireBuildFile(t, current, "current")
			if _, err := os.Stat(filepath.Join(cfg.VaultRoot, "current")); !os.IsNotExist(err) {
				t.Fatalf("build wrote into vault: %v", err)
			}
		})
	}
}

func TestComputeGenerationUsesRelativePathsAndAllContent(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, root := range []string{first, second} {
		writeBuildFixture(t, filepath.Join(root, "wiki", "Note.MD"), "upper")
		writeBuildFixture(t, filepath.Join(root, "wiki", "other.md"), "lower")
		writeBuildFixture(t, filepath.Join(root, "assets", "image.png"), "asset")
	}
	generation := func(root string) string {
		t.Helper()
		gen, err := computeGeneration(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		return gen
	}
	before := generation(first)
	if got := generation(second); got != before {
		t.Errorf("same content at different locations: %s != %s", got, before)
	}
	writeBuildFixture(t, filepath.Join(first, "wiki", "Note.MD"), "changed upper")
	afterNote := generation(first)
	if afterNote == before {
		t.Error("uppercase Markdown changes must affect generation")
	}
	writeBuildFixture(t, filepath.Join(first, "assets", "image.png"), "changed asset")
	if got := generation(first); got == afterNote {
		t.Error("asset changes must affect generation")
	}
}

func TestComputeGenerationSeparatesPathsFromContent(t *testing.T) {
	root := t.TempDir()
	writeBuildFixture(t, filepath.Join(root, "a.md"), "bc.mdcontent")
	first, err := computeGeneration(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
		t.Fatal(err)
	}
	writeBuildFixture(t, filepath.Join(root, "a.mdbc.md"), "content")
	second, err := computeGeneration(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different path/content boundaries must not share a generation")
	}
}

func TestRunHonorsBuildLock(t *testing.T) {
	cfg, disc := buildFixture(t)
	current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
	writeBuildFixture(t, current, "current")
	if err := os.Mkdir(filepath.Join(cfg.BuildRoot, ".build.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(cfg, disc, RunOptions{}); err == nil {
		t.Fatal("a build with an existing lock must fail")
	}
	requireBuildFile(t, current, "current")
}

func TestRunCancellationPreservesCurrentAndCleansStaging(t *testing.T) {
	for _, beforeRun := range []bool{true, false} {
		t.Run(fmt.Sprint(beforeRun), func(t *testing.T) {
			cfg, disc := buildFixture(t)
			current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
			writeBuildFixture(t, current, "current")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if beforeRun {
				cancel()
			}
			_, err := Run(cfg, disc, RunOptions{Context: ctx, Progress: buildProgressFunc(func(p []byte) (int, error) {
				requireBuildFile(t, current, "current")
				cancel()
				return len(p), nil
			})})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run error = %v, want context.Canceled", err)
			}
			requireBuildFile(t, current, "current")
			for _, name := range []string{"staging", ".build.lock"} {
				if _, err := os.Stat(filepath.Join(cfg.BuildRoot, name)); !os.IsNotExist(err) {
					t.Fatalf("%s remains: %v", name, err)
				}
			}
		})
	}
}

func TestRunSerializesWhileKeepingCurrentAvailable(t *testing.T) {
	cfg, disc := buildFixture(t)
	current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
	writeBuildFixture(t, current, "current")
	_, err := Run(cfg, disc, RunOptions{Progress: buildProgressFunc(func(p []byte) (int, error) {
		requireBuildFile(t, current, "current")
		if _, err := Run(cfg, disc, RunOptions{}); err == nil {
			t.Error("competing build acquired the lock")
		}
		return len(p), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BuildRoot, ".build.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock remains: %v", err)
	}
}

func TestGenerationIgnoresBuildManifest(t *testing.T) {
	root := t.TempDir()
	writeBuildFixture(t, filepath.Join(root, "wiki", "note.md"), "note")
	first, err := computeGeneration(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	writeBuildFixture(t, filepath.Join(root, "build.manifest"), "built: timestamp\n")
	second, err := computeGeneration(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("manifest changed generation: %s != %s", first, second)
	}
}

func TestRunWriteFailuresKeepPreviousBuild(t *testing.T) {
	for _, failAt := range []string{"note", "attachment", "manifest"} {
		t.Run(failAt, func(t *testing.T) {
			cfg, disc := buildFixture(t)
			current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
			writeBuildFixture(t, current, "current")
			if failAt == "attachment" {
				writeBuildFixture(t, disc.Notes[0].AbsPath, "![[image.png]]\n")
				writeBuildFixture(t, filepath.Join(disc.SourceRoot, "image.png"), "image")
			}
			_, err := Run(cfg, disc, RunOptions{Progress: buildProgressFunc(func(p []byte) (int, error) {
				staging := filepath.Join(cfg.BuildRoot, "staging")
				switch failAt {
				case "note":
					if err := os.Mkdir(filepath.Join(staging, cfg.SourceRoot, "index.md"), 0o755); err != nil {
						t.Fatal(err)
					}
				case "attachment":
					assets := filepath.Join(staging, cfg.Build.AssetsRoot)
					if err := os.Remove(assets); err != nil {
						t.Fatal(err)
					}
					writeBuildFixture(t, assets, "blocks asset output")
				case "manifest":
					if err := os.Mkdir(filepath.Join(staging, "build.manifest"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				return len(p), nil
			})})
			if err == nil {
				t.Fatal("output failure should fail the build")
			}
			requireBuildFile(t, current, "current")
			if _, err := os.Stat(filepath.Join(cfg.BuildRoot, "staging")); !os.IsNotExist(err) {
				t.Fatalf("failed staging remains: %v", err)
			}
		})
	}
}

func TestPromotionFailureRestoresCurrent(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current", "sentinel")
	writeBuildFixture(t, current, "current")
	// An absent staging directory forces the promotion rename itself to fail,
	// after the previous current has been moved to backup.
	if err := promoteBuild(context.Background(), root); err == nil {
		t.Fatal("missing staging should fail promotion")
	}
	requireBuildFile(t, current, "current")
}

func TestRunReportsCleanupFailureWithoutFollowingSymlink(t *testing.T) {
	cfg, disc := buildFixture(t)
	sentinel := filepath.Join(cfg.VaultRoot, "sentinel")
	writeBuildFixture(t, sentinel, "vault")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Run(cfg, disc, RunOptions{Context: ctx, Progress: buildProgressFunc(func(p []byte) (int, error) {
		staging := filepath.Join(cfg.BuildRoot, "staging")
		if err := os.RemoveAll(staging); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(cfg.VaultRoot, staging); err != nil {
			t.Skip(err)
		}
		cancel()
		return len(p), nil
	})})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "clean failed staging") {
		t.Fatalf("cleanup and cancellation errors must both be reported: %v", err)
	}
	requireBuildFile(t, sentinel, "vault")
}

func TestRestoreBackupBuildReportsConflictingCurrent(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current", "sentinel")
	backup := filepath.Join(root, "backup", "sentinel")
	writeBuildFixture(t, current, "unexpected current")
	writeBuildFixture(t, backup, "previous current")
	if err := restoreBackupBuild(root); err == nil {
		t.Fatal("restoring over an unexpected current must report a conflict")
	}
	requireBuildFile(t, current, "unexpected current")
	requireBuildFile(t, backup, "previous current")
}

func TestRestoreBackupBuildReportsMissingBackup(t *testing.T) {
	if err := restoreBackupBuild(t.TempDir()); err == nil {
		t.Fatal("missing backup must be reported as failed restoration")
	}
}

func TestRunRejectsNoteReplacedWithOutsideSymlink(t *testing.T) {
	cfg, disc := buildFixture(t)
	current := filepath.Join(cfg.BuildRoot, "current", "sentinel")
	writeBuildFixture(t, current, "previous build")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeBuildFixture(t, outside, "SYNTHETIC OUTSIDE NOTE\n")
	changed := false
	_, err := Run(cfg, disc, RunOptions{Progress: buildProgressFunc(func(p []byte) (int, error) {
		if !changed {
			changed = true
			if err := os.Remove(disc.Notes[0].AbsPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, disc.Notes[0].AbsPath); err != nil {
				t.Skip(err)
			}
		}
		return len(p), nil
	})})
	if err == nil {
		t.Fatal("a note replaced with an outside symlink must fail the build")
	}
	requireBuildFile(t, current, "previous build")
	for _, name := range []string{"staging", ".build.lock"} {
		if _, err := os.Lstat(filepath.Join(cfg.BuildRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains after failure: %v", name, err)
		}
	}
}

func TestRunReadsOriginalSourceWhenRootIsReplaced(t *testing.T) {
	cfg, disc := buildFixture(t)
	writeBuildFixture(t, disc.Notes[0].AbsPath, "---\ntitle: Original title\n---\n# Original note\n[[Original title]]\n")
	outside := t.TempDir()
	writeBuildFixture(t, filepath.Join(outside, "index.md"), "---\ntitle: Outside title\n---\nSYNTHETIC OUTSIDE NOTE\n")
	changed := false
	res, err := Run(cfg, disc, RunOptions{Progress: buildProgressFunc(func(p []byte) (int, error) {
		if !changed {
			changed = true
			if err := os.Rename(disc.SourceRoot, disc.SourceRoot+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, disc.SourceRoot); err != nil {
				t.Skip(err)
			}
		}
		return len(p), nil
	})})
	if err != nil {
		t.Fatalf("the anchored original source should remain readable: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(res.StagingDir, cfg.SourceRoot, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# Original note") || strings.Contains(string(body), "SYNTHETIC OUTSIDE NOTE") {
		t.Fatalf("build did not retain the original source root: %s", body)
	}
	if !strings.Contains(string(body), "[Original title](index.md)") {
		t.Fatalf("note index did not use the original source metadata: %s", body)
	}
}
