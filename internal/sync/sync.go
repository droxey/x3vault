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
	localFiles := map[string]int64{}
	err = filepath.Walk(localCurrent, func(p string, info os.FileInfo, err error) error {
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
		localFiles[rel] = info.Size()
		return nil
	})
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
	for rel, size := range localFiles {
		rsize, ok := remoteFiles[rel]
		if !ok || rsize != size {
			uploadRels = append(uploadRels, rel)
		}
	}
	sort.Strings(uploadRels)
	for _, rel := range uploadRels {
		plan.Uploads = append(plan.Uploads, PlanOp{
			Op:   "upload",
			Path: path.Join(root, rel),
			Size: localFiles[rel],
		})
		dir := path.Dir(rel)
		if dir != "." && dir != "/" {
			plan.Mkdirs = append(plan.Mkdirs, PlanOp{Op: "mkdir", Path: path.Join(root, dir)})
		}
	}
	var deleteRels []string
	for rel := range remoteFiles {
		if _, ok := localFiles[rel]; !ok {
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
	return plan, nil
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
			res.Errors = append(res.Errors, fmt.Sprintf("mkdir %s: %v", d, err))
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
			res.Errors = append(res.Errors, fmt.Sprintf("read %s: %v", localPath, err))
			continue
		}
		dir := path.Dir(op.Path)
		name := path.Base(op.Path)
		if err := t.Upload(dir, name, data); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("upload %s: %v", op.Path, err))
			continue
		}
		res.Uploaded++
		fmt.Fprintf(os.Stderr, "  uploaded %s\n", op.Path)
	}
	for _, op := range plan.Deletes {
		if dryRun {
			fmt.Fprintf(os.Stderr, "  delete %s\n", op.Path)
			res.Deleted++
			continue
		}
		if err := t.Delete(op.Path, op.Type); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("delete %s: %v", op.Path, err))
			continue
		}
		res.Deleted++
		fmt.Fprintf(os.Stderr, "  deleted %s\n", op.Path)
	}
	return res
}

func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
