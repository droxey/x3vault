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

	"github.com/droxey/x3vault/internal/config"
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

func DeviceInit(t *Transport, root, tool string) error {
	root = path.Clean(root)
	if root == "" || root == "/" {
		return fmt.Errorf("device root must be an absolute path (e.g. %s)", config.DefaultDeviceRoot)
	}
	if !strings.HasPrefix(root, "/") {
		return fmt.Errorf("device root must start with / (got %q)", root)
	}
	if err := ensureDeviceRoot(t, root); err != nil {
		return fmt.Errorf("ensure device root: %w", err)
	}
	if tool == "" {
		tool = config.DefaultOwnershipTool
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
			Tool:      tool,
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

func ensureDeviceRoot(t *Transport, root string) error {
	parts := strings.Split(strings.Trim(root, "/"), "/")
	cur := "/"
	for _, part := range parts {
		if part == "" {
			continue
		}
		entries, err := t.List(cur)
		if err != nil {
			return err
		}
		exists := false
		for _, e := range entries {
			if e.Name == part && e.IsDirectory {
				exists = true
				break
			}
		}
		if !exists {
			if err := t.Mkdir(cur, part); err != nil {
				return err
			}
		}
		cur = path.Join(cur, part)
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

func BuildPlan(t *Transport, root, localCurrent string, opts Options) (*Plan, error) {
	root = path.Clean(root)
	owned, err := HasOwnership(t, root)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, fmt.Errorf("no ownership marker at %s/%s — run: x3vault device init", root, OwnershipMarker)
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
		if needsUpload(t, root, rel, localHash, localCurrent, remoteFiles, remoteManifest, opts) {
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
	if opts.CleanEmptyDirs {
		appendEmptyDirDeletes(plan, localHashes, remoteFiles, deleteRels, remoteDirs, root)
	}
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

func needsUpload(t *Transport, root, rel, localHash, localCurrent string, remoteFiles map[string]int64, remoteManifest *HashManifest, opts Options) bool {
	if opts.HashManifest {
		if remoteHash, ok := remoteManifest.Files[rel]; ok && remoteHash == localHash {
			return false
		}
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

func ApplyPlan(t *Transport, plan *Plan, localCurrent string, dryRun bool, opts Options) *SyncResult {
	res := &SyncResult{}
	root := path.Clean(opts.DeviceRoot)
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
			if res.onError(opts, fmt.Sprintf("mkdir %s: %v", d, err)) {
				return res
			}
			continue
		}
	}
	for _, op := range plan.Uploads {
		rel := strings.TrimPrefix(op.Path, root+"/")
		if rel == op.Path {
			rel = strings.TrimPrefix(op.Path, root)
			rel = strings.TrimPrefix(rel, "/")
		}
		localPath := filepath.Join(localCurrent, filepath.FromSlash(rel))
		if dryRun {
			fmt.Fprintf(os.Stderr, "  upload %s (%d bytes)\n", op.Path, op.Size)
			res.Uploaded++
			continue
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			if res.onError(opts, fmt.Sprintf("read %s: %v", localPath, err)) {
				return res
			}
			continue
		}
		dir := path.Dir(op.Path)
		name := path.Base(op.Path)
		if err := t.Upload(dir, name, data); err != nil {
			if res.onError(opts, fmt.Sprintf("upload %s: %v", op.Path, err)) {
				return res
			}
			continue
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
			if res.onError(opts, fmt.Sprintf("delete %s: %v", op.Path, err)) {
				return res
			}
			continue
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
			if res.onError(opts, fmt.Sprintf("rmdir %s: %v", op.Path, err)) {
				return res
			}
			continue
		}
		res.Deleted++
		fmt.Fprintf(os.Stderr, "  rmdir %s\n", op.Path)
	}
	if !dryRun && len(res.Errors) == 0 && opts.HashManifest {
		if err := writeHashManifest(t, root, localCurrent); err != nil {
			if res.onError(opts, fmt.Sprintf("update hash manifest: %v", err)) {
				return res
			}
		}
	}
	return res
}

func (res *SyncResult) onError(opts Options, msg string) bool {
	res.Errors = append(res.Errors, msg)
	return opts.FailFast
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
