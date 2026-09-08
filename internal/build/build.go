package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/markdown"
	"github.com/droxey/x3vault/internal/obsidian"
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

func Run(cfg *config.Config, disc *vault.Discovery) (*Result, error) {
	staging := filepath.Join(cfg.BuildRoot, "staging")
	wikiOut := filepath.Join(staging, cfg.SourceRoot)
	assetOut := filepath.Join(staging, cfg.Build.AssetsRoot)

	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(wikiOut, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(assetOut, 0o755); err != nil {
		return nil, err
	}

	noteRefs := make([]markdown.NoteRef, len(disc.Notes))
	for i, n := range disc.Notes {
		noteRefs[i] = markdown.NoteRef{RelPath: n.RelPath, AbsPath: n.AbsPath}
	}
	indexResult := markdown.BuildNoteIndex(noteRefs)
	attachmentAbs := resolveAttachmentFolder(cfg)

	opts := markdown.NormalizeOpts{
		VaultRoot:           cfg.VaultRoot,
		SourceRoot:          disc.SourceRoot,
		SourceRel:           cfg.SourceRoot,
		AssetsRoot:          cfg.Build.AssetsRoot,
		NoteIndex:           indexResult.Index,
		AttachmentFolder:    attachmentAbs,
		IsExcludedVaultPath: cfg.IsExcludedVaultPath,
		AssetOutDir:         assetOut,
	}

	res := &Result{StagingDir: staging}
	assetSeen := map[string]bool{}

	for _, n := range disc.Notes {
		norm, err := markdown.Normalize(n.AbsPath, n.RelPath, opts)
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
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		header := fmt.Sprintf("<!-- x3vault: %s -->\n", n.RelPath)
		if norm.Title != "" {
			header += fmt.Sprintf("<!-- title: %s -->\n", norm.Title)
		}
		body := header + "\n" + norm.Body
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
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

	gen, err := computeGeneration(wikiOut)
	if err != nil {
		return res, err
	}
	res.Generation = gen

	manifest := fmt.Sprintf("generation: %s\nnotes: %d\nassets: %d\nbuilt: %s\n",
		gen, res.Notes, res.Assets, time.Now().UTC().Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(staging, "build.manifest"), []byte(manifest), 0o644)

	current := filepath.Join(cfg.BuildRoot, "current")
	_ = os.RemoveAll(current)
	if err := os.Rename(staging, current); err != nil {
		return res, fmt.Errorf("promote staging: %w", err)
	}
	res.StagingDir = current
	return res, nil
}

func resolveAttachmentFolder(cfg *config.Config) string {
	if cfg.Build.AttachmentFolder != "" {
		return filepath.Join(cfg.VaultRoot, filepath.FromSlash(cfg.Build.AttachmentFolder))
	}
	if !cfg.Build.ReadObsidianConfig {
		return ""
	}
	rel := obsidian.AttachmentFolder(cfg.VaultRoot)
	return obsidian.ResolveAttachmentPath(cfg.VaultRoot, rel)
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
