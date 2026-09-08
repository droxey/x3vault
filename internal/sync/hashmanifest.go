package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
)

const (
	HashManifestPath   = "_meta/file-hashes.json"
	HashManifestSchema = 1
)

type HashManifest struct {
	Schema int               `json:"schema"`
	Files  map[string]string `json:"files"`
}

func NewHashManifest() *HashManifest {
	return &HashManifest{Schema: HashManifestSchema, Files: map[string]string{}}
}

type localSnapshot struct {
	hashes map[string]string
	sizes  map[string]int64
}

// openLocalRoot confines all subsequent file reads even if a directory is renamed
// or an entry is replaced with a symlink while syncing.
func openLocalRoot(localCurrent string) (*os.Root, error) {
	localCurrent = filepath.Clean(localCurrent)
	info, err := os.Lstat(localCurrent)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("local build must be a real directory: %s", localCurrent)
	}
	root, err := os.OpenRoot(localCurrent)
	if err != nil {
		return nil, err
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		root.Close()
		return nil, fmt.Errorf("local build directory changed while opening: %s", localCurrent)
	}
	return root, nil
}

func snapshotLocal(ctx context.Context, localCurrent string) (*localSnapshot, error) {
	root, err := openLocalRoot(localCurrent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	snapshot := &localSnapshot{hashes: map[string]string{}, sizes: map[string]int64{}}
	err = fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if err := validateRelativePath(rel); err != nil {
			return err
		}
		if isMetadata(rel) {
			return fmt.Errorf("local build contains reserved metadata path %q", rel)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local build contains symlink %q", rel)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("local build contains non-regular file %q", rel)
		}
		if rel == "build.manifest" {
			return nil
		}
		file, err := root.Open(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			file.Close()
			return fmt.Errorf("local build contains non-regular file %q", rel)
		}
		hash := sha256.New()
		count, readErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		snapshot.hashes[rel] = hex.EncodeToString(hash.Sum(nil))
		snapshot.sizes[rel] = count
		return nil
	})
	return snapshot, err
}

func HashLocalTree(localCurrent string) (map[string]string, error) {
	snapshot, err := snapshotLocal(context.Background(), localCurrent)
	if err != nil {
		return nil, err
	}
	return snapshot.hashes, nil
}

func (s *localSnapshot) matches(other *localSnapshot) bool {
	return maps.Equal(s.hashes, other.hashes) && maps.Equal(s.sizes, other.sizes)
}

func readPlannedFile(localCurrent, rel, expectedHash string) ([]byte, error) {
	root, err := openLocalRoot(localCurrent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("local build file is no longer regular: %s", rel)
	}
	file, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("local build file is no longer regular: %s", rel)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expectedHash {
		return nil, fmt.Errorf("local build changed since planning: %s; run sync again", rel)
	}
	return data, nil
}

func (m *HashManifest) Encode() ([]byte, error) {
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	m.Schema = HashManifestSchema
	return json.MarshalIndent(m, "", "  ")
}

func ParseHashManifest(data []byte) (*HashManifest, error) {
	var m HashManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Schema != HashManifestSchema {
		return nil, fmt.Errorf("unsupported hash manifest schema %d", m.Schema)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	for rel, hash := range m.Files {
		if err := validateRelativePath(rel); err != nil {
			return nil, err
		}
		if isMetadata(rel) {
			return nil, fmt.Errorf("reserved path in hash manifest: %s", rel)
		}
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid SHA-256 hash for %s", rel)
		}
	}
	return &m, nil
}

func LoadRemoteHashManifest(ctx context.Context, t FileTransport, root string) (*HashManifest, error) {
	data, err := t.ReadFile(ctx, path.Join(root, HashManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return NewHashManifest(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hash manifest: %w", err)
	}
	manifest, err := ParseHashManifest(data)
	if err != nil {
		return nil, fmt.Errorf("invalid hash manifest (disable sync.hash_manifest to rebuild the cache): %w", err)
	}
	return manifest, nil
}
