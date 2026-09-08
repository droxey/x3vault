package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/markdown"
	"github.com/droxey/x3vault/internal/pathutil"
	"github.com/droxey/x3vault/internal/vault"
)

type Result struct {
	Generation string
	Notes      int
	Assets     int
	Warnings   []string
	Errors     []string
	StagingDir string
}

const buildCurrentDir = "current"
const buildBackupDir = "backup"

type RunOptions struct {
	Context  context.Context
	Progress io.Writer
}

func Run(cfg *config.Config, disc *vault.Discovery, opts RunOptions) (res *Result, runErr error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buildRoot, err := validateBuildInputs(cfg, disc)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	noteReader, err := markdown.OpenNoteReader(disc.SourceRoot)
	if err != nil {
		return nil, fmt.Errorf("open source notes: %w", err)
	}
	defer func() {
		if err := noteReader.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close source notes: %w", err))
		}
	}()
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}

	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create build root: %w", err)
	}
	lock := filepath.Join(buildRoot, ".build.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		return nil, fmt.Errorf("acquire build lock %s (another build may be running): %w", lock, err)
	}
	defer func() {
		if err := validateManagedDir(buildRoot, ".build.lock"); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("release build lock: %w", err))
		} else if err := os.Remove(lock); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("release build lock: %w", err))
		}
	}()

	staging := filepath.Join(buildRoot, "staging")
	wikiOut := filepath.Join(staging, cfg.SourceRoot)
	assetOut := filepath.Join(staging, cfg.Build.AssetsRoot)

	write := func(path string) error {
		return vault.AssertBuildWritePath(path, buildRoot)
	}

	if err := validateManagedDir(buildRoot, "staging"); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(staging); err != nil {
		return nil, fmt.Errorf("clean old staging: %w", err)
	}
	promoted := false
	defer func() {
		if !promoted {
			if err := validateManagedDir(buildRoot, "staging"); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("clean failed staging: %w", err))
			} else if err := os.RemoveAll(staging); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("clean failed staging: %w", err))
			}
		}
	}()
	if err := write(wikiOut); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(wikiOut, 0o755); err != nil {
		return nil, err
	}
	if err := write(assetOut); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(assetOut, 0o755); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(progress, "building %d notes in %s\n", len(disc.Notes), staging); err != nil {
		return nil, fmt.Errorf("write build progress: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wikiDirs := cfg.Wiki
	wikiDirs.Normalize()
	var noteRefs []markdown.NoteRef
	for _, n := range disc.Notes {
		if wikiDirs.ShouldIncludeRelPath(n.RelPath) {
			noteRefs = append(noteRefs, markdown.NoteRef{RelPath: n.RelPath, AbsPath: n.AbsPath})
		}
	}
	indexResult := markdown.BuildNoteIndex(ctx, noteRefs, noteReader)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normOpts := markdown.NormalizeOpts{
		Context:                 ctx,
		NoteReader:              noteReader,
		VaultRoot:               cfg.VaultRoot,
		SourceRoot:              disc.SourceRoot,
		SourceRel:               cfg.SourceRoot,
		AssetsRoot:              cfg.Build.AssetsRoot,
		NoteIndex:               indexResult.Index,
		AttachmentFolder:        cfg.ResolveAttachmentFolderAbs(),
		AttachmentFolderForNote: cfg.ResolveAttachmentFolderForNoteAbs,
		IsExcludedVaultPath:     cfg.IsExcludedVaultPath,
		ShouldIncludeSourceRel:  wikiDirs.ShouldIncludeRelPath,
		AssetOutDir:             assetOut,
		AssetCache:              make(map[string]markdown.AssetRef),
	}

	res = &Result{StagingDir: staging}
	assetSeen := map[string]bool{}

	for _, n := range noteRefs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		norm, err := markdown.Normalize(n.AbsPath, n.RelPath, normOpts)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", n.RelPath, err))
			continue
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		for _, w := range norm.Warnings {
			res.Warnings = append(res.Warnings, n.RelPath+": "+w)
		}
		for _, u := range norm.Unresolved {
			res.Warnings = append(res.Warnings, n.RelPath+": unresolved [["+u+"]]")
		}

		dest := filepath.Join(wikiOut, n.RelPath)
		if err := write(dest); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if err := os.WriteFile(dest, []byte(norm.Body), 0o644); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		res.Notes++

		for _, a := range norm.Assets {
			if !assetSeen[a.DeviceRel] {
				assetSeen[a.DeviceRel] = true
				res.Assets++
			}
		}
	}

	if len(res.Errors) > 0 {
		return res, fmt.Errorf("build failed with %d note error(s)", len(res.Errors))
	}

	gen, err := computeGeneration(ctx, staging)
	if err != nil {
		return res, err
	}
	res.Generation = gen

	manifest := fmt.Sprintf("generation: %s\nnotes: %d\nassets: %d\nbuilt: %s\n",
		gen, res.Notes, res.Assets, time.Now().UTC().Format(time.RFC3339))
	manifestPath := filepath.Join(staging, "build.manifest")
	if err := write(manifestPath); err != nil {
		return res, err
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return res, fmt.Errorf("write manifest: %w", err)
	}

	if err := promoteBuild(ctx, buildRoot); err != nil {
		return res, err
	}
	promoted = true
	res.StagingDir = filepath.Join(buildRoot, buildCurrentDir)
	return res, nil
}

// validateBuildInputs runs before any build mutation, including lock creation.
func validateBuildInputs(cfg *config.Config, disc *vault.Discovery) (string, error) {
	if cfg == nil || disc == nil {
		return "", fmt.Errorf("build requires config and discovery")
	}
	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid build config: %w", err)
	}
	if err := config.ValidateBuildRootOutsideVault(cfg.BuildRoot, cfg.VaultRoot); err != nil {
		return "", err
	}
	buildRoot, err := pathutil.Canonical(cfg.BuildRoot)
	if err != nil {
		return "", err
	}
	vaultRoot, err := pathutil.Canonical(cfg.VaultRoot)
	if err != nil {
		return "", err
	}
	sourceRoot, err := pathutil.Canonical(filepath.Join(cfg.VaultRoot, cfg.SourceRoot))
	if err != nil {
		return "", err
	}
	discoveredSource, err := pathutil.Canonical(disc.SourceRoot)
	if err != nil {
		return "", err
	}
	if !pathutil.ContainedIn(sourceRoot, vaultRoot) || sourceRoot != discoveredSource {
		return "", fmt.Errorf("discovered source must match the configured source inside the vault")
	}
	for _, name := range []string{"staging", buildCurrentDir, buildBackupDir, ".build.lock"} {
		if err := validateManagedDir(buildRoot, name); err != nil {
			return "", err
		}
	}
	for _, note := range disc.Notes {
		if !filepath.IsLocal(filepath.FromSlash(note.RelPath)) || strings.Contains(note.RelPath, `\`) {
			return "", fmt.Errorf("invalid relative note path %q", note.RelPath)
		}
		for _, segment := range strings.Split(note.RelPath, "/") {
			if segment == ".." || segment == "." || segment == "" {
				return "", fmt.Errorf("invalid relative note path %q", note.RelPath)
			}
		}
		notePath, err := pathutil.Canonical(note.AbsPath)
		if err != nil {
			return "", err
		}
		expected, err := pathutil.Canonical(filepath.Join(sourceRoot, filepath.FromSlash(note.RelPath)))
		if err != nil {
			return "", err
		}
		if notePath != expected || !pathutil.ContainedIn(notePath, sourceRoot) {
			return "", fmt.Errorf("note escapes or does not match source: %s", note.RelPath)
		}
	}
	return buildRoot, nil
}

// Build-managed directories must be actual directories, never symlink aliases.
func validateManagedDir(buildRoot, name string) error {
	path := filepath.Join(buildRoot, name)
	if err := vault.AssertBuildWritePath(path, buildRoot); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect build %s: %w", name, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("build %s must be a directory without symlinks", name)
	}
	return nil
}

// Promotion is the only phase that moves current. If it cannot commit, restore
// the old current and include any restoration failure in the returned error.
func promoteBuild(ctx context.Context, buildRoot string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, name := range []string{"staging", buildCurrentDir, buildBackupDir} {
		if err := validateManagedDir(buildRoot, name); err != nil {
			return err
		}
	}
	backedUp, err := backupCurrentBuild(buildRoot)
	if err != nil {
		return err
	}
	err = ctx.Err()
	if err == nil {
		if renameErr := os.Rename(filepath.Join(buildRoot, "staging"), filepath.Join(buildRoot, buildCurrentDir)); renameErr != nil {
			err = fmt.Errorf("promote staging: %w", renameErr)
		}
	}
	if err != nil && backedUp {
		err = errors.Join(err, restoreBackupBuild(buildRoot))
	}
	return err
}

// backupCurrentBuild moves build/current to build/backup during promotion.
// Returns true when an existing current build was moved to backup.
func backupCurrentBuild(buildRoot string) (bool, error) {
	for _, name := range []string{buildCurrentDir, buildBackupDir} {
		if err := validateManagedDir(buildRoot, name); err != nil {
			return false, err
		}
	}
	current := filepath.Join(buildRoot, buildCurrentDir)
	if _, err := os.Stat(current); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat current build: %w", err)
	}
	backup := filepath.Join(buildRoot, buildBackupDir)
	if err := os.RemoveAll(backup); err != nil {
		return false, fmt.Errorf("remove old backup: %w", err)
	}
	if err := os.Rename(current, backup); err != nil {
		return false, fmt.Errorf("backup current build: %w", err)
	}
	return true, nil
}

// restoreBackupBuild moves build/backup back to build/current after a failed build.
func restoreBackupBuild(buildRoot string) error {
	for _, name := range []string{buildCurrentDir, buildBackupDir} {
		if err := validateManagedDir(buildRoot, name); err != nil {
			return err
		}
	}
	current := filepath.Join(buildRoot, buildCurrentDir)
	if _, err := os.Stat(current); err == nil {
		return fmt.Errorf("restore backup build: current unexpectedly exists")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat current build: %w", err)
	}
	backup := filepath.Join(buildRoot, buildBackupDir)
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf("stat backup build: %w", err)
	}
	if err := os.Rename(backup, current); err != nil {
		return fmt.Errorf("restore backup build: %w", err)
	}
	return nil
}

func computeGeneration(ctx context.Context, buildDir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(buildDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(buildDir, path)
		if err != nil {
			return err
		}
		if rel == "build.manifest" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular build output: %s", rel)
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		file, err := os.Open(filepath.Join(buildDir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		content := sha256.New()
		_, copyErr := io.Copy(content, contextReader{ctx: ctx, reader: file})
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return "", err
		}
		// Length-prefix paths and use fixed-size content hashes so path/content
		// boundaries cannot be confused with a different collection of files.
		fmt.Fprintf(h, "%d:", len(rel))
		h.Write([]byte(rel))
		h.Write(content.Sum(nil))
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	return "g-" + sum, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
