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
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

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
	Uploads []PlanOp
	Deletes []PlanOp
	Mkdirs  []PlanOp

	// Bind execution to the root, local content, and operations verified by BuildPlan.
	root         string
	tool         string
	localCurrent string
	snapshot     *localSnapshot
	uploads      []PlanOp
	deletes      []PlanOp
	mkdirs       []PlanOp
}

type SyncResult struct {
	Uploaded int
	Deleted  int
	Errors   []string
}

func ownershipTool(tool string) string {
	if strings.TrimSpace(tool) == "" {
		return config.DefaultOwnershipTool
	}
	return strings.TrimSpace(tool)
}

func isMetadata(rel string) bool {
	return rel == "_meta" || strings.HasPrefix(rel, "_meta/")
}

func validateEntryName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("invalid device entry name %q", name)
	}
	return nil
}

func validateRelativePath(rel string) error {
	for _, segment := range strings.Split(rel, "/") {
		if err := validateEntryName(segment); err != nil {
			return err
		}
	}
	return nil
}

func listEntries(ctx context.Context, t FileTransport, dir string) ([]FileEntry, error) {
	entries, err := t.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if err := validateEntryName(entry.Name); err != nil {
			return nil, err
		}
		if seen[entry.Name] {
			return nil, fmt.Errorf("duplicate entry %q in %s", entry.Name, dir)
		}
		if entry.Size < 0 {
			return nil, fmt.Errorf("negative size for %q in %s", entry.Name, dir)
		}
		seen[entry.Name] = true
	}
	return entries, nil
}

func DeviceInit(ctx context.Context, t FileTransport, root, tool string) error {
	root, err := config.CanonicalDeviceRoot(root)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("missing device transport")
	}
	tool = ownershipTool(tool)
	if err := ensureDeviceRoot(ctx, t, root); err != nil {
		return fmt.Errorf("ensure device root: %w", err)
	}
	entries, err := listEntries(ctx, t, root)
	if err != nil {
		return fmt.Errorf("inspect device root: %w", err)
	}
	owned, err := HasOwnership(ctx, t, root, tool)
	if err != nil {
		return err
	}
	if owned {
		return nil
	}
	metaExists := false
	for _, entry := range entries {
		if entry.Name != "_meta" || !entry.IsDirectory {
			return fmt.Errorf("refusing: %s is nonempty and unowned; clear it manually or choose another root", root)
		}
		metaExists = true
	}
	if metaExists {
		metadata, err := listEntries(ctx, t, path.Join(root, "_meta"))
		if err != nil {
			return fmt.Errorf("inspect unowned metadata: %w", err)
		}
		if len(metadata) != 0 {
			return fmt.Errorf("refusing: %s/_meta is nonempty and unowned", root)
		}
	} else if err := t.Mkdir(ctx, root, "_meta"); err != nil {
		return err
	}
	own := Ownership{Schema: OwnershipSchema, Tool: tool, CreatedAt: time.Now().UTC().Format(time.RFC3339), Root: root}
	data, err := json.MarshalIndent(own, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ownership marker: %w", err)
	}
	if err := t.Upload(ctx, path.Join(root, "_meta"), "ownership.json", data); err != nil {
		return fmt.Errorf("write ownership marker: %w", err)
	}
	return nil
}

func ensureDeviceRoot(ctx context.Context, t FileTransport, root string) error {
	cur := "/"
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		entries, err := listEntries(ctx, t, cur)
		if err != nil {
			return err
		}
		exists := false
		for _, entry := range entries {
			if entry.Name == part {
				if !entry.IsDirectory {
					return fmt.Errorf("device path is a file: %s", path.Join(cur, part))
				}
				exists = true
			}
		}
		if !exists {
			if err := t.Mkdir(ctx, cur, part); err != nil {
				return err
			}
		}
		cur = path.Join(cur, part)
	}
	return nil
}

// HasOwnership validates the marker, including its configured tool and root.
// The optional tool preserves the default-tool form used by existing callers.
func HasOwnership(ctx context.Context, t FileTransport, root string, expectedTool ...string) (bool, error) {
	root, err := config.CanonicalDeviceRoot(root)
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, fmt.Errorf("missing device transport")
	}
	tool := config.DefaultOwnershipTool
	if len(expectedTool) > 0 {
		tool = ownershipTool(expectedTool[0])
	}
	data, err := t.ReadFile(ctx, path.Join(root, OwnershipMarker))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read ownership marker: %w", err)
	}
	var own Ownership
	if err := json.Unmarshal(data, &own); err != nil {
		return false, fmt.Errorf("invalid ownership marker: %w", err)
	}
	markerRoot, rootErr := config.CanonicalDeviceRoot(own.Root)
	if own.Schema != OwnershipSchema || own.Tool != tool || rootErr != nil || markerRoot != root {
		return false, fmt.Errorf("ownership marker does not match schema %d, tool %q, and root %q", OwnershipSchema, tool, root)
	}
	return true, nil
}

func BuildPlan(ctx context.Context, t FileTransport, root, localCurrent string, opts Options) (*Plan, error) {
	root, err := config.CanonicalDeviceRoot(root)
	if err != nil {
		return nil, err
	}
	optionRoot, err := config.CanonicalDeviceRoot(opts.DeviceRoot)
	if err != nil || optionRoot != root {
		return nil, fmt.Errorf("plan root does not match device options")
	}
	owned, err := HasOwnership(ctx, t, root, opts.OwnershipTool)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, fmt.Errorf("no ownership marker at %s/%s — run: x3vault device init", root, OwnershipMarker)
	}
	localCurrent, err = filepath.Abs(localCurrent)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotLocal(ctx, localCurrent)
	if err != nil {
		return nil, fmt.Errorf("hash local build: %w", err)
	}
	remoteManifest := NewHashManifest()
	if opts.HashManifest {
		remoteManifest, err = LoadRemoteHashManifest(ctx, t, root)
		if err != nil {
			return nil, err
		}
	}
	remoteFiles := map[string]int64{}
	remoteDirs := map[string]bool{}
	var walkRemote func(string) error
	walkRemote = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := listEntries(ctx, t, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			full := path.Join(dir, entry.Name)
			rel := strings.TrimPrefix(full, root+"/")
			if isMetadata(rel) {
				if rel == "_meta" && !entry.IsDirectory {
					return fmt.Errorf("reserved metadata path is not a directory")
				}
				continue
			}
			if entry.IsDirectory {
				remoteDirs[rel] = true
				if err := walkRemote(full); err != nil {
					return err
				}
			} else {
				remoteFiles[rel] = entry.Size
			}
		}
		return nil
	}
	if err := walkRemote(root); err != nil {
		return nil, fmt.Errorf("walk remote: %w", err)
	}
	plan := &Plan{root: root, tool: ownershipTool(opts.OwnershipTool), localCurrent: localCurrent, snapshot: snapshot}
	localFiles := make([]string, 0, len(snapshot.hashes))
	for rel := range snapshot.hashes {
		localFiles = append(localFiles, rel)
	}
	sort.Strings(localFiles)
	missingDirs := map[string]bool{}
	for _, rel := range localFiles {
		if remoteDirs[rel] {
			return nil, typeConflict(path.Join(root, rel))
		}
		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			if _, exists := remoteFiles[dir]; exists {
				return nil, typeConflict(path.Join(root, dir))
			}
		}
		upload, err := needsUpload(ctx, t, root, rel, snapshot.hashes[rel], snapshot.sizes[rel], remoteFiles, remoteManifest, opts)
		if err != nil {
			return nil, err
		}
		if !upload {
			continue
		}
		plan.Uploads = append(plan.Uploads, PlanOp{Op: "upload", Path: path.Join(root, rel), Size: snapshot.sizes[rel]})
		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			if !remoteDirs[dir] {
				missingDirs[dir] = true
			}
		}
	}
	for dir := range missingDirs {
		plan.Mkdirs = append(plan.Mkdirs, PlanOp{Op: "mkdir", Path: path.Join(root, dir)})
	}
	sortOps(plan.Mkdirs, false)
	var deleteRels []string
	for rel := range remoteFiles {
		if _, exists := snapshot.hashes[rel]; !exists {
			deleteRels = append(deleteRels, rel)
		}
	}
	sortPaths(deleteRels, true)
	for _, rel := range deleteRels {
		plan.Deletes = append(plan.Deletes, PlanOp{Op: "delete", Path: path.Join(root, rel), Type: "file"})
	}
	if opts.CleanEmptyDirs {
		appendEmptyDirDeletes(plan, snapshot.hashes, remoteFiles, deleteRels, remoteDirs, root)
	}
	plan.uploads = slices.Clone(plan.Uploads)
	plan.deletes = slices.Clone(plan.Deletes)
	plan.mkdirs = slices.Clone(plan.Mkdirs)
	return plan, nil
}

func typeConflict(p string) error {
	return fmt.Errorf("file/directory type conflict at %s; move the conflicting device entry manually or choose another device root", p)
}

func sortPaths(paths []string, deepestFirst bool) {
	sort.Slice(paths, func(i, j int) bool { return pathLess(paths[i], paths[j], deepestFirst) })
}

func sortOps(ops []PlanOp, deepestFirst bool) {
	sort.Slice(ops, func(i, j int) bool { return pathLess(ops[i].Path, ops[j].Path, deepestFirst) })
}

func pathLess(a, b string, deepestFirst bool) bool {
	da, db := strings.Count(a, "/"), strings.Count(b, "/")
	if da == db {
		return a < b
	}
	if deepestFirst {
		return da > db
	}
	return da < db
}

func appendEmptyDirDeletes(plan *Plan, localHashes map[string]string, remoteFiles map[string]int64, fileDeletes []string, remoteDirs map[string]bool, root string) {
	deletingFiles := map[string]bool{}
	for _, rel := range fileDeletes {
		deletingFiles[rel] = true
	}
	remaining := map[string]int64{}
	for rel, size := range remoteFiles {
		if !deletingFiles[rel] {
			remaining[rel] = size
		}
	}
	var dirs []string
	for dir := range remoteDirs {
		dirs = append(dirs, dir)
	}
	sortPaths(dirs, true)
	deletingDirs := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || isMetadata(dir) || hasPathPrefix(localHashes, dir) || hasPathPrefix(remaining, dir) {
			continue
		}
		undeletedChild := false
		for child := range remoteDirs {
			if strings.HasPrefix(child, dir+"/") && !deletingDirs[child] {
				undeletedChild = true
				break
			}
		}
		if !undeletedChild {
			deletingDirs[dir] = true
			plan.Deletes = append(plan.Deletes, PlanOp{Op: "delete", Path: path.Join(root, dir), Type: "directory"})
		}
	}
}

func hasPathPrefix[V any](files map[string]V, dir string) bool {
	for f := range files {
		if strings.HasPrefix(f, dir+"/") {
			return true
		}
	}
	return false
}

func needsUpload(ctx context.Context, t FileTransport, root, rel, localHash string, localSize int64, remoteFiles map[string]int64, remoteManifest *HashManifest, opts Options) (bool, error) {
	remoteSize, exists := remoteFiles[rel]
	if !exists || localSize != remoteSize {
		return true, nil
	}
	if opts.HashManifest && remoteManifest != nil && remoteManifest.Files[rel] == localHash {
		return false, nil
	}
	data, err := t.ReadFile(ctx, path.Join(root, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("compare remote file %s: %w", rel, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) != localHash, nil
}

func validatePlan(plan *Plan, localCurrent string, opts Options) error {
	if plan == nil || plan.snapshot == nil {
		return fmt.Errorf("sync plan must be produced by BuildPlan")
	}
	root, err := config.CanonicalDeviceRoot(opts.DeviceRoot)
	if err != nil {
		return err
	}
	current, err := filepath.Abs(localCurrent)
	if err != nil {
		return err
	}
	if root != plan.root || ownershipTool(opts.OwnershipTool) != plan.tool || current != plan.localCurrent {
		return fmt.Errorf("sync plan does not match the device root, ownership tool, or local build")
	}
	for _, ops := range [][]PlanOp{plan.Mkdirs, plan.Uploads, plan.Deletes} {
		for _, op := range ops {
			if !strings.HasPrefix(op.Path, root+"/") {
				return fmt.Errorf("operation outside owned root: %s", op.Path)
			}
			rel := strings.TrimPrefix(op.Path, root+"/")
			if err := validateRelativePath(rel); err != nil {
				return err
			}
			if isMetadata(rel) {
				return fmt.Errorf("operation targets reserved metadata: %s", op.Path)
			}
		}
	}
	if !slices.Equal(plan.Uploads, plan.uploads) || !slices.Equal(plan.Deletes, plan.deletes) || !slices.Equal(plan.Mkdirs, plan.mkdirs) {
		return fmt.Errorf("sync plan operations changed after planning")
	}
	return nil
}

func verifyLocalSnapshot(ctx context.Context, plan *Plan) error {
	snapshot, err := snapshotLocal(ctx, plan.localCurrent)
	if err != nil {
		return fmt.Errorf("verify local build: %w", err)
	}
	if !plan.snapshot.matches(snapshot) {
		return fmt.Errorf("local build changed since planning; run sync again")
	}
	return nil
}

func invalidateHashManifest(ctx context.Context, t FileTransport, root string) error {
	entries, err := listEntries(ctx, t, path.Join(root, "_meta"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name == "file-hashes.json" {
			if entry.IsDirectory {
				return fmt.Errorf("hash manifest path is a directory")
			}
			if err := t.Delete(ctx, path.Join(root, HashManifestPath), "file"); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func ApplyPlan(ctx context.Context, t FileTransport, plan *Plan, localCurrent string, dryRun bool, opts Options) *SyncResult {
	res := &SyncResult{}
	stop := func(err error) *SyncResult { res.Errors = append(res.Errors, err.Error()); return res }
	if err := validatePlan(plan, localCurrent, opts); err != nil {
		return stop(err)
	}
	if err := verifyLocalSnapshot(ctx, plan); err != nil {
		return stop(err)
	}
	owned, err := HasOwnership(ctx, t, plan.root, plan.tool)
	if err != nil {
		return stop(err)
	}
	if !owned {
		return stop(fmt.Errorf("ownership marker disappeared since planning"))
	}
	progress := opts.Progress
	if progress == nil {
		progress = io.Discard
	}
	if dryRun {
		for _, op := range plan.Mkdirs {
			fmt.Fprintf(progress, "  mkdir %s\n", op.Path)
		}
		for _, op := range plan.Uploads {
			fmt.Fprintf(progress, "  upload %s (%d bytes)\n", op.Path, op.Size)
			res.Uploaded++
		}
		for _, op := range plan.Deletes {
			action := "delete"
			if op.Type == "directory" {
				action = "rmdir"
			}
			fmt.Fprintf(progress, "  %s %s\n", action, op.Path)
			res.Deleted++
		}
		return res
	}
	if !opts.HashManifest || len(plan.Mkdirs)+len(plan.Uploads)+len(plan.Deletes) > 0 {
		// Invalidate even with hash_manifest disabled: an old receipt must never
		// survive a partial update or certify data changed by an uncached sync.
		if err := invalidateHashManifest(ctx, t, plan.root); err != nil {
			return stop(fmt.Errorf("invalidate hash manifest: %w", err))
		}
	}
	for _, op := range plan.Mkdirs {
		if err := t.Mkdir(ctx, path.Dir(op.Path), path.Base(op.Path)); err != nil {
			if res.onError(opts, fmt.Sprintf("mkdir %s: %v", op.Path, err)) {
				return res
			}
		}
	}
	for _, op := range plan.Uploads {
		rel := strings.TrimPrefix(op.Path, plan.root+"/")
		data, err := readPlannedFile(plan.localCurrent, rel, plan.snapshot.hashes[rel])
		if err != nil {
			if res.onError(opts, fmt.Sprintf("read %s: %v", rel, err)) {
				return res
			}
			continue
		}
		if err := t.Upload(ctx, path.Dir(op.Path), path.Base(op.Path), data); err != nil {
			if res.onError(opts, fmt.Sprintf("upload %s: %v", op.Path, err)) {
				return res
			}
			continue
		}
		res.Uploaded++
		fmt.Fprintf(progress, "  uploaded %s\n", op.Path)
	}
	// Continue-on-error may transfer other files, but never remove old content
	// after a failed upload or after the build changed underneath this plan.
	if len(res.Errors) > 0 {
		return res
	}
	if err := verifyLocalSnapshot(ctx, plan); err != nil {
		return stop(err)
	}
	for _, op := range plan.Deletes {
		if op.Type == "directory" && len(res.Errors) > 0 {
			break
		}
		if err := t.Delete(ctx, op.Path, op.Type); err != nil {
			if res.onError(opts, fmt.Sprintf("delete %s: %v", op.Path, err)) {
				return res
			}
			continue
		}
		res.Deleted++
		fmt.Fprintf(progress, "  deleted %s\n", op.Path)
	}
	if len(res.Errors) == 0 && opts.HashManifest {
		// These hashes describe bytes verified during planning or actually uploaded,
		// never a fresh hash of a potentially different local build.
		data, err := (&HashManifest{Files: plan.snapshot.hashes}).Encode()
		if err != nil {
			return stop(err)
		}
		if err := t.Upload(ctx, path.Join(plan.root, "_meta"), "file-hashes.json", data); err != nil {
			return stop(fmt.Errorf("update hash manifest: %w", err))
		}
	}
	return res
}

func (res *SyncResult) onError(opts Options, msg string) bool {
	res.Errors = append(res.Errors, msg)
	return opts.FailFast
}
