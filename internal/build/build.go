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

func Run(cfgVaultRoot, cfgSourceRoot, cfgBuildRoot string, disc *vault.Discovery) (*Result, error) {
	staging := filepath.Join(cfgBuildRoot, "staging")
	wikiOut := filepath.Join(staging, "wiki")
	assetOut := filepath.Join(staging, "assets")

	// Clean previous staging
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(wikiOut, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(assetOut, 0o755); err != nil {
		return nil, err
	}

	// Build note index
	type pair struct{ RelPath, AbsPath string }
	pairs := make([]pair, len(disc.Notes))
	for i, n := range disc.Notes {
		pairs[i] = pair{n.RelPath, n.AbsPath}
	}
	// convert for BuildNoteIndex
	idxNotes := make([]struct{ RelPath, AbsPath string }, len(pairs))
	for i, p := range pairs {
		idxNotes[i] = struct{ RelPath, AbsPath string }{p.RelPath, p.AbsPath}
	}
	noteIndex := markdown.BuildNoteIndex(idxNotes)

	opts := markdown.NormalizeOpts{
		VaultRoot:   cfgVaultRoot,
		SourceRoot:  disc.SourceRoot,
		NoteIndex:   noteIndex,
		AssetOutDir: assetOut,
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

		// Emit note
		dest := filepath.Join(wikiOut, n.RelPath)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		// Prepend a minimal readable header
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

	// Generation ID from content hashes of all emitted notes (sorted)
	gen, err := computeGeneration(wikiOut)
	if err != nil {
		return res, err
	}
	res.Generation = gen

	// Write a simple manifest
	manifest := fmt.Sprintf("generation: %s\nnotes: %d\nassets: %d\nbuilt: %s\n",
		gen, res.Notes, res.Assets, time.Now().UTC().Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(staging, "build.manifest"), []byte(manifest), 0o644)

	// Promote staging → current
	current := filepath.Join(cfgBuildRoot, "current")
	_ = os.RemoveAll(current)
	if err := os.Rename(staging, current); err != nil {
		// fallback copy on cross-device
		return res, fmt.Errorf("promote staging: %w", err)
	}
	res.StagingDir = current
	return res, nil
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
