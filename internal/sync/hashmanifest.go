package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

const (
	HashManifestPath = "_meta/file-hashes.json"
	HashManifestSchema = 1
)

type HashManifest struct {
	Schema int               `json:"schema"`
	Files  map[string]string `json:"files"`
}

func NewHashManifest() *HashManifest {
	return &HashManifest{
		Schema: HashManifestSchema,
		Files:  map[string]string{},
	}
}

func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func HashLocalTree(localCurrent string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.Walk(localCurrent, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localCurrent, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "build.manifest" {
			return nil
		}
		sum, err := FileSHA256(p)
		if err != nil {
			return err
		}
		hashes[rel] = sum
		return nil
	})
	return hashes, err
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
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	if m.Schema != 0 && m.Schema != HashManifestSchema {
		return nil, fmt.Errorf("unsupported hash manifest schema %d", m.Schema)
	}
	return &m, nil
}

func LoadRemoteHashManifest(t *Transport, root string) (*HashManifest, error) {
	path := pathJoin(root, HashManifestPath)
	data, err := t.ReadFile(path)
	if err != nil {
		return NewHashManifest(), nil
	}
	return ParseHashManifest(data)
}

func pathJoin(root, rel string) string {
	return path.Join(root, rel)
}
