package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	OwnershipMarker = "_meta/ownership.json"
	OwnershipSchema = 1
)

type Ownership struct {
	Schema    int    `json:"schema"`
	Tool      string `json:"tool"`
	CreatedAt string `json:"created_at"`
	Root      string `json:"root"`
}

type PlanOp struct {
	Op   string
	Path string
	Type string
	Size int64
}

type Plan struct {
	Uploads  []PlanOp
	Deletes  []PlanOp
	Mkdirs   []PlanOp
	Warnings []string
}

type SyncResult struct {
	Uploaded int
	Deleted  int
	Skipped  int
	Errors   []string
}

func DeviceInit(t *Transport, root string) error {
	root = path.Clean(root)
	if root != "/x3vault" {
		return fmt.Errorf("v0 only supports root /x3vault (got %q)", root)
	}
	entries, err := t.List("/")
	if err != nil {
		return fmt.Errorf("list root: %w", err)
	}
	hasRoot := false
	for _, e := range entries {
		if e.Name == "x3vault" && e.IsDirectory {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		if err := t.Mkdir("/", "x3vault"); err != nil {
			return fmt.Errorf("create /x3vault: %w", err)
		}
	}
	metaEntries, err := t.List(root + "/_meta")
	owned := false
	if err == nil {
		for _, e := range metaEntries {
			if e.Name == "ownership.json" && !e.IsDirectory {
				owned = true
				break
			}
		}
	}
	if !owned {
		rootEntries, err := t.List(root)
		if err == nil && len(rootEntries) > 0 {
			for _, e := range rootEntries {
				if e.Name != "_meta" {
					return fmt.Errorf("refusing: %s is nonempty and unowned; clear it manually or choose another root", root)
				}
			}
		}
		if err := t.Mkdir(root, "_meta"); err != nil {
			return err
		}
		own := Ownership{
			Schema:    OwnershipSchema,
			Tool:      "x3vault",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Root:      root,
		}
		data, _ := json.MarshalIndent(own, "", "  ")
		if err := t.Upload(root+"/_meta", "ownership.json", data); err != nil {
			return fmt.Errorf("write ownership marker: %w", err)
		}
	}
	return nil
}

func HasOwnership(t *Transport, root string) (bool, error) {
	entries, err := t.List(root + "/_meta")
	if err != nil {
		return false, nil
	}
	for _, e := range entries {
		if e.Name == "ownership.json" && !e.IsDirectory {
			return true, nil
		}
	}
	return false, nil
}

func BuildPlan(t *Transport, root, localCurrent string) (*Plan, error) {
	root = path.Clean(root)
	owned, err := HasOwnership(t, root)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, fmt.Errorf("no ownership marker at %s/_meta/ownership.json — run: x3vault device init", root)
	}
	plan := &Plan{}
	localHashes, err := HashLocalTree(localCurrent)
	if err != nil {
		return nil, fmt.Errorf("hash local build: %w", err)
	}
	remoteManifest, err := LoadRemoteHashManifest(t, root)
	if err != nil {
		return nil, err
	}

	remoteFiles := map[string]int64{}
	remoteDirs := map[string]bool{}
	var walkRemote func(dir string) error
	walkRemote = func(dir string) error {
		entries, err := t.List(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := path.Join(dir, e.Name)
			rel := strings.TrimPrefix(full, root+"/")
			if e.IsDirectory {
				if rel == "_meta" || strings.HasPrefix(rel, "_meta/") {
					continue
				}
				remoteDirs[rel] = true
				if err := walkRemote(full); err != nil {
					return err
				}
			} else {
				if strings.HasPrefix(rel, "_meta/") {
					continue
				}
				remoteFiles[rel] = e.Size
			}
		}
		return nil
	}
	if err := walkRemote(root); err != nil {
		return nil, fmt.Errorf("walk remote: %w", err)
	}
	var uploadRels []string
	for rel, localHash := range localHashes {
		if needsUpload(t, root, rel, localHash, localCurrent, remoteFiles, remoteManifest) {
			uploadRels = append(uploadRels, rel)
		}
	}
	sort.Strings(uploadRels)
	for _, rel := range uploadRels {
		localPath := filepath.Join(localCurrent, filepath.FromSlash(rel))
		info, err := os.Stat(localPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", localPath, err)
		}
		plan.Uploads = append(plan.Uploads, PlanOp{
			Op:   "upload",
			Path: path.Join(root, rel),
			Size: info.Size(),
		})
		dir := path.Dir(rel)
		if dir != "." && dir != "/" {
			plan.Mkdirs = append(plan.Mkdirs, PlanOp{Op: "mkdir", Path: path.Join(root, dir)})
		}
	}
	var deleteRels []string
	for rel := range remoteFiles {
		if _, ok := localHashes[rel]; !ok {
			deleteRels = append(deleteRels, rel)
		}
	}
	sort.Slice(deleteRels, func(i, j int) bool {
		return strings.Count(deleteRels[i], "/") > strings.Count(deleteRels[j], "/")
	})
	for _, rel := range deleteRels {
		plan.Deletes = append(plan.Deletes, PlanOp{
			Op:   "delete",
			Path: path.Join(root, rel),
			Type: "file",
		})
	}
	appendEmptyDirDeletes(plan, localHashes, remoteFiles, deleteRels, remoteDirs, root)
	return plan, nil
}

func appendEmptyDirDeletes(plan *Plan, localHashes map[string]string, remoteFiles map[string]int64, fileDeletes []string, remoteDirs map[string]bool, root string) {
	deleteFiles := map[string]bool{}
	for _, rel := range fileDeletes {
		deleteFiles[rel] = true
	}
	remaining := map[string]int64{}
	for rel, size := range remoteFiles {
		if !deleteFiles[rel] {
			remaining[rel] = size
		}
	}

	plannedDirs := map[string]bool{}
	for {
		added := false
		var candidates []string
		for dir := range remoteDirs {
			if dir == "" || strings.HasPrefix(dir, "_meta") || plannedDirs[dir] {
				continue
			}
			if mapKeyHasPrefix(localHashes, dir) || hasPathPrefix(remaining, dir) {
				continue
			}
			if hasUndeletedSubdir(dir, remoteDirs, plannedDirs) {
				continue
			}
			candidates = append(candidates, dir)
		}
		sort.Slice(candidates, func(i, j int) bool {
			return strings.Count(candidates[i], "/") > strings.Count(candidates[j], "/")
		})
		for _, dir := range candidates {
			plannedDirs[dir] = true
			plan.Deletes = append(plan.Deletes, PlanOp{
				Op:   "delete",
				Path: path.Join(root, dir),
				Type: "directory",
			})
			added = true
		}
		if !added {
			break
		}
	}
}

func hasPathPrefix(files map[string]int64, dir string) bool {
	prefix := dir + "/"
	for f := range files {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func mapKeyHasPrefix(files map[string]string, dir string) bool {
	prefix := dir + "/"
	for f := range files {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func hasUndeletedSubdir(dir string, dirs, deleting map[string]bool) bool {
	prefix := dir + "/"
	for d := range dirs {
		if d != dir && strings.HasPrefix(d, prefix) && !deleting[d] {
			return true
		}
	}
	return false
}

func needsUpload(t *Transport, root, rel, localHash, localCurrent string, remoteFiles map[string]int64, remoteManifest *HashManifest) bool {
	if remoteHash, ok := remoteManifest.Files[rel]; ok && remoteHash == localHash {
		return false
	}
	rsize, exists := remoteFiles[rel]
	if !exists {
		return true
	}
	localPath := filepath.Join(localCurrent, filepath.FromSlash(rel))
	info, err := os.Stat(localPath)
	if err != nil || info.Size() != rsize {
		return true
	}
	remoteData, err := t.ReadFile(path.Join(root, rel))
	if err != nil {
		return true
	}
	sum := sha256.Sum256(remoteData)
	return hex.EncodeToString(sum[:]) != localHash
}

func ApplyPlan(t *Transport, plan *Plan, localCurrent string, dryRun bool) *SyncResult {
	res := &SyncResult{}
	root := "/x3vault"
	seenDir := map[string]bool{}
	var dirs []string
	for _, op := range plan.Mkdirs {
		if !seenDir[op.Path] {
			seenDir[op.Path] = true
			dirs = append(dirs, op.Path)
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], "/") < strings.Count(dirs[j], "/")
	})
	for _, d := range dirs {
		if dryRun {
			fmt.Fprintf(os.Stderr, "  mkdir %s\n", d)
			continue
		}
		parent, name := path.Split(strings.TrimSuffix(d, "/"))
		parent = strings.TrimSuffix(parent, "/")
		if parent == "" {
			parent = "/"
		}
		if err := t.Mkdir(parent, name); err != nil {
			return res.fail(fmt.Sprintf("mkdir %s: %v", d, err))
		}
	}
	for _, op := range plan.Uploads {
		rel := strings.TrimPrefix(op.Path, root+"/")
		localPath := filepath.Join(localCurrent, filepath.FromSlash(rel))
		if dryRun {
			fmt.Fprintf(os.Stderr, "  upload %s (%d bytes)\n", op.Path, op.Size)
			res.Uploaded++
			continue
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			return res.fail(fmt.Sprintf("read %s: %v", localPath, err))
		}
		dir := path.Dir(op.Path)
		name := path.Base(op.Path)
		if err := t.Upload(dir, name, data); err != nil {
			return res.fail(fmt.Sprintf("upload %s: %v", op.Path, err))
		}
		res.Uploaded++
		fmt.Fprintf(os.Stderr, "  uploaded %s\n", op.Path)
	}
	var fileDeletes, dirDeletes []PlanOp
	for _, op := range plan.Deletes {
		if op.Type == "directory" {
			dirDeletes = append(dirDeletes, op)
		} else {
			fileDeletes = append(fileDeletes, op)
		}
	}
	for _, op := range fileDeletes {
		if dryRun {
			fmt.Fprintf(os.Stderr, "  delete %s\n", op.Path)
			res.Deleted++
			continue
		}
		if err := t.Delete(op.Path, op.Type); err != nil {
			return res.fail(fmt.Sprintf("delete %s: %v", op.Path, err))
		}
		res.Deleted++
		fmt.Fprintf(os.Stderr, "  deleted %s\n", op.Path)
	}
	sort.Slice(dirDeletes, func(i, j int) bool {
		return strings.Count(dirDeletes[i].Path, "/") > strings.Count(dirDeletes[j].Path, "/")
	})
	for _, op := range dirDeletes {
		if dryRun {
			fmt.Fprintf(os.Stderr, "  rmdir %s\n", op.Path)
			res.Deleted++
			continue
		}
		if err := t.Delete(op.Path, op.Type); err != nil {
			return res.fail(fmt.Sprintf("rmdir %s: %v", op.Path, err))
		}
		res.Deleted++
		fmt.Fprintf(os.Stderr, "  rmdir %s\n", op.Path)
	}
	if !dryRun && len(res.Errors) == 0 {
		if err := writeHashManifest(t, root, localCurrent); err != nil {
			return res.fail(fmt.Sprintf("update hash manifest: %v", err))
		}
	}
	return res
}

func (res *SyncResult) fail(msg string) *SyncResult {
	res.Errors = append(res.Errors, msg)
	return res
}

func writeHashManifest(t *Transport, root, localCurrent string) error {
	hashes, err := HashLocalTree(localCurrent)
	if err != nil {
		return err
	}
	manifest := &HashManifest{Files: hashes}
	data, err := manifest.Encode()
	if err != nil {
		return err
	}
	return t.Upload(root+"/_meta", "file-hashes.json", data)
}

func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
