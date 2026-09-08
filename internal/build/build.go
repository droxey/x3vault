package build

import (
	"crypto/sha256"
	"encoding/hex"
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
	Progress io.Writer
}

func Run(cfg *config.Config, disc *vault.Discovery, opts RunOptions) (*Result, error) {
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}

	backedUp, err := backupCurrentBuild(cfg.BuildRoot)
	if err != nil {
		return nil, err
	}
	if backedUp {
		fmt.Fprintf(progress, "backed up previous build to %s\n", filepath.Join(cfg.BuildRoot, buildBackupDir))
	}
	promoted := false
	if backedUp {
		defer func() {
			if !promoted {
				_ = os.RemoveAll(filepath.Join(cfg.BuildRoot, "staging"))
				_ = restoreBackupBuild(cfg.BuildRoot)
			}
		}()
	}

	staging := filepath.Join(cfg.BuildRoot, "staging")
	wikiOut := filepath.Join(staging, cfg.SourceRoot)
	assetOut := filepath.Join(staging, cfg.Build.AssetsRoot)

	write := func(path string) error {
		return vault.AssertBuildWritePath(path, cfg.BuildRoot)
	}

	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(wikiOut, 0o755); err != nil {
		return nil, err
	}
	if err := write(wikiOut); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(assetOut, 0o755); err != nil {
		return nil, err
	}
	if err := write(assetOut); err != nil {
		return nil, err
	}

	noteRefs := make([]markdown.NoteRef, len(disc.Notes))
	for i, n := range disc.Notes {
		noteRefs[i] = markdown.NoteRef{RelPath: n.RelPath, AbsPath: n.AbsPath}
	}
	indexResult := markdown.BuildNoteIndex(noteRefs)
	attachmentAbs := cfg.ResolveAttachmentFolderAbs()
	wikiDirs := cfg.Wiki
	wikiDirs.Normalize()

	normOpts := markdown.NormalizeOpts{
		VaultRoot:              cfg.VaultRoot,
		SourceRoot:             disc.SourceRoot,
		SourceRel:              cfg.SourceRoot,
		AssetsRoot:             cfg.Build.AssetsRoot,
		NoteIndex:              indexResult.Index,
		AttachmentFolder:       attachmentAbs,
		IsExcludedVaultPath:    cfg.IsExcludedVaultPath,
		ShouldIncludeSourceRel: wikiDirs.ShouldIncludeRelPath,
		AssetOutDir:            assetOut,
	}

	res := &Result{StagingDir: staging}
	assetSeen := map[string]bool{}

	for _, n := range disc.Notes {
		norm, err := markdown.Normalize(n.AbsPath, n.RelPath, normOpts)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", n.RelPath, err))
			continue
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

	if err := pruneIgnoredOutput(wikiOut, wikiDirs); err != nil {
		return res, fmt.Errorf("prune ignored output: %w", err)
	}

	gen, err := computeGeneration(wikiOut)
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

	current := filepath.Join(cfg.BuildRoot, buildCurrentDir)
	if err := os.Rename(staging, current); err != nil {
		return res, fmt.Errorf("promote staging: %w", err)
	}
	promoted = true
	res.StagingDir = current
	return res, nil
}

// backupCurrentBuild moves build/current to build/backup before a new build.
// Returns true when an existing current build was moved to backup.
func backupCurrentBuild(buildRoot string) (bool, error) {
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
	current := filepath.Join(buildRoot, buildCurrentDir)
	if _, err := os.Stat(current); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat current build: %w", err)
	}
	backup := filepath.Join(buildRoot, buildBackupDir)
	if _, err := os.Stat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat backup build: %w", err)
	}
	if err := os.Rename(backup, current); err != nil {
		return fmt.Errorf("restore backup build: %w", err)
	}
	return nil
}

// pruneIgnoredOutput removes any files or directories under ignored_dirs from build output.
func pruneIgnoredOutput(wikiOut string, dirs config.WikiDirs) error {
	dirs.Normalize()
	for _, ig := range dirs.Ignored {
		if err := os.RemoveAll(filepath.Join(wikiOut, filepath.FromSlash(ig))); err != nil {
			return err
		}
	}
	return filepath.WalkDir(wikiOut, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(wikiOut, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !dirs.ShouldWalkDir(rel) {
				return os.RemoveAll(path)
			}
			return nil
		}
		if !dirs.ShouldIncludeRelPath(rel) {
			return os.Remove(path)
		}
		return nil
	})
}

func computeGeneration(wikiDir string) (string, error) {
	var paths []string
	err := filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.ToSlash(p)))
		h.Write(data)
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	return "g-" + sum, nil
}
