package markdown

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/droxey/x3vault/internal/pathutil"
)

var errAssetUnavailable = errors.New("attachment unavailable")

func assetCandidates(target, noteDir string, opts NormalizeOpts) []string {
	base := filepath.Base(target)
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	add(filepath.Join(opts.SourceRoot, noteDir, target))
	if opts.AttachmentFolder != "" {
		add(filepath.Join(opts.AttachmentFolder, target))
		add(filepath.Join(opts.AttachmentFolder, base))
	}
	add(filepath.Join(opts.SourceRoot, target))
	add(filepath.Join(opts.VaultRoot, target))
	add(filepath.Join(opts.VaultRoot, "Attachments", base))
	add(filepath.Join(opts.VaultRoot, "assets", base))
	assetsRoot := opts.AssetsRoot
	if assetsRoot == "" {
		assetsRoot = "assets"
	}
	add(filepath.Join(opts.SourceRoot, assetsRoot, base))
	return out
}
func pathUnderDir(p, dir string) bool {
	if dir == "" {
		return false
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	dAbs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return pathutil.ContainedIn(pAbs, dAbs)
}

// Apply the policy both to the authored path and its resolved target.
func assetPathAllowed(p, vault, source, attachment string, opts NormalizeOpts) bool {
	if !pathUnderDir(p, vault) {
		return false
	}
	rel, err := filepath.Rel(vault, p)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, part := range strings.Split(rel, "/") {
		if part == ".obsidian" || part == ".git" || part == ".xte" || part == ".x3vault" {
			return false
		}
	}
	if pathUnderDir(p, source) && opts.ShouldIncludeSourceRel != nil {
		sourceRel, err := filepath.Rel(source, p)
		if err != nil || !opts.ShouldIncludeSourceRel(filepath.ToSlash(sourceRel)) {
			return false
		}
	}
	return opts.IsExcludedVaultPath == nil || !opts.IsExcludedVaultPath(rel) || pathUnderDir(p, attachment)
}
func resolveAsset(target, noteDir string, opts NormalizeOpts) (*AssetRef, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vault, err := pathutil.Canonical(opts.VaultRoot)
	if err != nil {
		return nil, err
	}
	source, err := pathutil.Canonical(opts.SourceRoot)
	if err != nil {
		return nil, err
	}
	attachment := ""
	if opts.AttachmentFolder != "" {
		attachment, err = pathutil.Canonical(opts.AttachmentFolder)
		if err != nil {
			return nil, err
		}
	}
	// Absolute and parent references are still checked canonically per candidate,
	// so a harmless ../ sibling within the vault remains supported.
	for _, candidate := range assetCandidates(target, noteDir, opts) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			if os.IsNotExist(err) || errors.Is(err, os.ErrInvalid) {
				continue
			}
			return nil, err
		}
		found, err = filepath.Abs(found)
		if err != nil {
			return nil, err
		}
		if !assetPathAllowed(candidate, opts.VaultRoot, opts.SourceRoot, opts.AttachmentFolder, opts) || !assetPathAllowed(found, vault, source, attachment, opts) {
			continue
		}
		rel, err := filepath.Rel(vault, found)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(found)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if cached, ok := opts.AssetCache[found]; ok {
			return &cached, nil
		}
		// OpenRoot prevents a symlink replacement from escaping the vault between
		// checking the candidate and opening it.
		root, err := os.OpenRoot(vault)
		if err != nil {
			return nil, err
		}
		in, err := root.Open(filepath.FromSlash(rel))
		closeRootErr := root.Close()
		if err != nil {
			return nil, err
		}
		if closeRootErr != nil {
			_ = in.Close()
			return nil, closeRootErr
		}
		opened, err := in.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			_ = in.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("attachment changed while opening %s", target)
		}
		data, readErr := io.ReadAll(&contextReader{ctx: ctx, r: in})
		closeErr := in.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		hash := fmt.Sprintf("%x", sum)
		name := sanitizeName(filepath.Base(found))
		assetsRoot := opts.AssetsRoot
		if assetsRoot == "" {
			assetsRoot = "assets"
		}
		asset := AssetRef{SourceAbs: found, DeviceRel: filepath.ToSlash(filepath.Join(assetsRoot, hash, name)), HashPrefix: hash}
		if opts.AssetOutDir != "" {
			if err := writeAsset(ctx, opts.AssetOutDir, hash, name, data); err != nil {
				return nil, err
			}
		}
		if opts.AssetCache != nil {
			opts.AssetCache[found] = asset
		}
		return &asset, nil
	}
	return nil, errAssetUnavailable
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func writeAsset(ctx context.Context, root, hash, name string, data []byte) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonRoot, err := pathutil.Canonical(root)
	if err != nil {
		return err
	}
	dest := filepath.Join(root, hash, name)
	canonDest, err := pathutil.Canonical(dest)
	if err != nil {
		return err
	}
	if !pathUnderDir(canonDest, canonRoot) {
		return fmt.Errorf("asset output escapes assets directory")
	}
	if info, err := os.Lstat(dest); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("asset output is not a regular file: %s", dest)
		}
		existing, err := os.ReadFile(dest)
		if err != nil {
			return err
		}
		if sha256.Sum256(existing) != sha256.Sum256(data) {
			return fmt.Errorf("asset output content mismatch: %s", dest)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".asset-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, dest)
}
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`/\\<>:"|?*`, r) {
			b.WriteByte('_')
		} else if r == ' ' {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	result := strings.Trim(b.String(), ". ")
	if result == "" {
		return "attachment"
	}
	return result
}
