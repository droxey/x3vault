package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type memoryDevice struct {
	dirs        map[string]bool
	files       map[string][]byte
	listErr     map[string]error
	readErr     map[string]error
	uploadErr   map[string]error
	deleteErr   map[string]error
	extra       map[string][]FileEntry
	writes      []string
	afterUpload func(string)
}

func newMemoryDevice() *memoryDevice {
	return &memoryDevice{dirs: map[string]bool{"/": true, "/ereader": true, "/ereader/_meta": true}, files: map[string][]byte{"/ereader/_meta/ownership.json": []byte(`{"schema":1,"tool":"ereader","root":"/ereader"}`)}, listErr: map[string]error{}, readErr: map[string]error{}, uploadErr: map[string]error{}, deleteErr: map[string]error{}, extra: map[string][]FileEntry{}}
}
func (m *memoryDevice) Status(context.Context) (*Status, error) { return &Status{}, nil }
func (m *memoryDevice) List(ctx context.Context, dir string) ([]FileEntry, error) {
	if err := m.listErr[dir]; err != nil {
		return nil, err
	}
	if !m.dirs[dir] {
		return nil, fmt.Errorf("missing directory %s: %w", dir, fs.ErrNotExist)
	}
	out := append([]FileEntry{}, m.extra[dir]...)
	for p := range m.dirs {
		if p != dir && path.Dir(p) == dir {
			out = append(out, FileEntry{Name: path.Base(p), IsDirectory: true})
		}
	}
	for p, b := range m.files {
		if path.Dir(p) == dir {
			out = append(out, FileEntry{Name: path.Base(p), Size: int64(len(b))})
		}
	}
	return out, nil
}
func (m *memoryDevice) Mkdir(ctx context.Context, parent, name string) error {
	p := path.Join(parent, name)
	m.writes = append(m.writes, "mkdir "+p)
	if !m.dirs[parent] {
		return fmt.Errorf("parent missing: %s", parent)
	}
	if _, ok := m.files[p]; ok {
		return fmt.Errorf("file already exists: %s", p)
	}
	m.dirs[p] = true
	return nil
}
func (m *memoryDevice) Upload(ctx context.Context, dir, name string, b []byte) error {
	p := path.Join(dir, name)
	m.writes = append(m.writes, "upload "+p)
	if err := m.uploadErr[p]; err != nil {
		return err
	}
	if !m.dirs[dir] {
		return fmt.Errorf("parent missing: %s", dir)
	}
	if m.dirs[p] {
		return fmt.Errorf("is directory: %s", p)
	}
	m.files[p] = append([]byte{}, b...)
	if m.afterUpload != nil {
		m.afterUpload(p)
	}
	return nil
}
func (m *memoryDevice) Delete(ctx context.Context, p, typ string) error {
	m.writes = append(m.writes, "delete "+p)
	if err := m.deleteErr[p]; err != nil {
		return err
	}
	if typ == "directory" {
		if !m.dirs[p] {
			return fs.ErrNotExist
		}
		for child := range m.dirs {
			if strings.HasPrefix(child, p+"/") {
				return fmt.Errorf("directory not empty")
			}
		}
		for child := range m.files {
			if strings.HasPrefix(child, p+"/") {
				return fmt.Errorf("directory not empty")
			}
		}
		delete(m.dirs, p)
	} else {
		if _, exists := m.files[p]; !exists {
			return fs.ErrNotExist
		}
		delete(m.files, p)
	}
	return nil
}
func (m *memoryDevice) ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := m.readErr[p]; err != nil {
		return nil, err
	}
	if b, ok := m.files[p]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("not found %s: %w", p, fs.ErrNotExist)
}
func localBuildFile(t *testing.T, rel string, b []byte) string {
	t.Helper()
	local := t.TempDir()
	p := filepath.Join(local, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	return local
}
func syncTestOptions() Options {
	return Options{DeviceRoot: "/ereader", OwnershipTool: "ereader", HashManifest: true, CleanEmptyDirs: true, FailFast: true}
}
func TestManifestMissingRemote(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("hello"))
	h, _ := HashLocalTree(local)
	b, _ := (&HashManifest{Files: h}).Encode()
	m.files["/ereader/_meta/file-hashes.json"] = b
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Uploads) != 1 {
		t.Fatalf("missing remote file wrongly skipped: %+v", p)
	}
}
func TestManifestDisabledMalformed(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/_meta/file-hashes.json"] = []byte("broken")
	local := localBuildFile(t, "note.md", []byte("hello"))
	opts := syncTestOptions()
	opts.HashManifest = false
	if _, err := BuildPlan(context.Background(), m, "/ereader", local, opts); err != nil {
		t.Fatalf("disabled manifest blocks sync: %v", err)
	}
}
func TestNestedParentDirectories(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "wiki/deep/note.md", []byte("hello"))
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) > 0 {
		t.Fatalf("nested first sync fails: %v; mkdir plan: %v", r.Errors, p.Mkdirs)
	}
}
func TestInvalidOwnership(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/_meta/ownership.json"] = []byte(`{"schema":999,"tool":"foreign","root":"/other"}`)
	owned, err := HasOwnership(context.Background(), m, "/ereader")
	if err == nil && owned {
		t.Fatal("invalid foreign ownership marker is accepted")
	}
}
func TestInitListFailure(t *testing.T) {
	m := newMemoryDevice()
	delete(m.files, "/ereader/_meta/ownership.json")
	m.listErr["/ereader"] = fmt.Errorf("unreadable root")
	err := DeviceInit(context.Background(), m, "/ereader", "ereader")
	if err == nil || len(m.writes) > 0 {
		t.Fatalf("claims root after failed emptiness check: err=%v writes=%v", err, m.writes)
	}
}
func TestMetaTraversal(t *testing.T) {
	m := newMemoryDevice()
	m.extra["/ereader"] = []FileEntry{{Name: "../_meta/ownership.json"}}
	local := localBuildFile(t, "note.md", []byte("hello"))
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		return
	}
	for _, op := range p.Deletes {
		if op.Path == "/ereader/_meta/ownership.json" {
			t.Fatalf("malformed entry schedules ownership marker deletion: %+v", p.Deletes)
		}
	}
}
func TestRehashAfterUpload(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("before"))
	m.afterUpload = func(p string) {
		if p == "/ereader/note.md" {
			if err := os.WriteFile(filepath.Join(local, "note.md"), []byte("changed"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) == 0 {
		t.Fatal("local mutation was not reported")
	}
	p, err = BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Uploads) == 0 {
		t.Fatalf("manifest certifies unuploaded changed bytes; remote=%s local=changed", m.files["/ereader/note.md"])
	}
}
func TestTransportFullDownload(t *testing.T) {
	want := bytes.Repeat([]byte("x"), 100000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(want) }))
	defer srv.Close()
	got, err := NewTransport(srv.URL, time.Second).ReadFile(context.Background(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("successful download truncated from %d to %d bytes", len(want), len(got))
	}
}
func TestLargeManifest(t *testing.T) {
	manifest := NewHashManifest()
	for i := 0; i < 1000; i++ {
		manifest.Files[fmt.Sprintf("wiki/%04d.md", i)] = fmt.Sprintf("%064x", i)
	}
	b, _ := json.Marshal(manifest)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(b) }))
	defer srv.Close()
	_, err := LoadRemoteHashManifest(context.Background(), NewTransport(srv.URL, time.Second), "/ereader")
	if err != nil {
		t.Fatalf("valid %d-byte manifest breaks loading: %v", len(b), err)
	}
}
func TestFileDirectoryTransition(t *testing.T) {
	for _, directoryFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(directoryFirst), func(t *testing.T) {
			m := newMemoryDevice()
			var local string
			if directoryFirst {
				m.dirs["/ereader/wiki"] = true
				m.files["/ereader/wiki/old.md"] = []byte("old")
				local = localBuildFile(t, "wiki", []byte("new"))
			} else {
				m.files["/ereader/wiki"] = []byte("old")
				local = localBuildFile(t, "wiki/note.md", []byte("new"))
			}
			_, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
			if err == nil || len(m.writes) > 0 {
				t.Fatalf("must refuse type conflict before mutation: err=%v writes=%v", err, m.writes)
			}
		})
	}
}
func TestSymlinkOutsideBuild(t *testing.T) {
	m := newMemoryDevice()
	local := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(local, "leak.md")); err != nil {
		t.Skip(err)
	}
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		return
	}
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) == 0 && string(m.files["/ereader/leak.md"]) == "private" {
		t.Fatal("uploaded data read from outside build tree through symlink")
	}
}
func TestMetaPrefixCleanup(t *testing.T) {
	p := &Plan{}
	appendEmptyDirDeletes(p, map[string]string{}, map[string]int64{}, nil, map[string]bool{"_metadata": true}, "/ereader")
	if len(p.Deletes) != 1 {
		t.Fatal("ordinary empty _metadata directory excluded from cleanup")
	}
}

func TestRejectsLocalMetadata(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "_meta/ownership.json", []byte("foreign"))
	if _, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions()); err == nil {
		t.Fatal("reserved local metadata accepted")
	}
}
func TestApplyRejectsMutatedPlan(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("hello"))
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	p.Deletes = append(p.Deletes, PlanOp{Op: "delete", Path: "/ereader/_meta/ownership.json", Type: "file"})
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) == 0 || len(m.writes) > 0 {
		t.Fatalf("invalid plan applied: errors=%v writes=%v", r.Errors, m.writes)
	}
}
func TestApplyRechecksOwnership(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("hello"))
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	m.files["/ereader/_meta/ownership.json"] = []byte(`{"schema":1,"tool":"foreign","root":"/ereader"}`)
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) == 0 || len(m.writes) > 0 {
		t.Fatalf("changed ownership ignored: errors=%v writes=%v", r.Errors, m.writes)
	}
}
func TestApplyRejectsChangedLocal(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("hello"))
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "note.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
	if len(r.Errors) == 0 || len(m.writes) > 0 {
		t.Fatalf("changed build accepted: errors=%v writes=%v", r.Errors, m.writes)
	}
}
func TestDisabledManifestInvalidatesBeforeWrites(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/_meta/file-hashes.json"] = []byte("ignored malformed cache")
	local := localBuildFile(t, "note.md", []byte("hello"))
	opts := syncTestOptions()
	opts.HashManifest = false
	p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, opts)
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if len(m.writes) < 2 || m.writes[0] != "delete /ereader/_meta/file-hashes.json" {
		t.Fatalf("receipt was not invalidated first: %v", m.writes)
	}
	if _, ok := m.files["/ereader/_meta/file-hashes.json"]; ok {
		t.Fatal("disabled manifest left stale cache")
	}
}
func TestMutationDuringApplyRetainsObsoleteFiles(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/old.md"] = []byte("keep until uploads verified")
	local := localBuildFile(t, "note.md", []byte("hello"))
	opts := syncTestOptions()
	opts.FailFast = false
	p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	m.afterUpload = func(p string) {
		if p == "/ereader/note.md" {
			_ = os.WriteFile(filepath.Join(local, "note.md"), []byte("changed"), 0644)
		}
	}
	r := ApplyPlan(context.Background(), m, p, local, false, opts)
	if len(r.Errors) == 0 {
		t.Fatal("build mutation not reported")
	}
	if _, ok := m.files["/ereader/old.md"]; !ok {
		t.Fatal("deleted old data despite local mutation")
	}
	if _, ok := m.files["/ereader/_meta/file-hashes.json"]; ok {
		t.Fatal("published an unverified receipt")
	}
}
func TestDryRunNoMutations(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/old.md"] = []byte("old")
	local := localBuildFile(t, "wiki/note.md", []byte("new"))
	opts := syncTestOptions()
	p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, true, opts)
	if len(r.Errors) > 0 || len(m.writes) > 0 {
		t.Fatalf("dry run errors=%v writes=%v", r.Errors, m.writes)
	}
}
func TestInvalidRemoteNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "nested/name", "back\\slash", "nul\x00", "newline\n"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			m := newMemoryDevice()
			m.extra["/ereader"] = []FileEntry{{Name: name}}
			local := localBuildFile(t, "note.md", []byte("new"))
			if _, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions()); err == nil {
				t.Fatal("invalid name accepted")
			}
		})
	}
}
func TestRejectsForgedPlans(t *testing.T) {
	for _, p := range []*Plan{nil, {}, {Deletes: []PlanOp{{Op: "delete", Path: "/outside", Type: "file"}}}, {Uploads: []PlanOp{{Op: "upload", Path: "/ereader/../secret"}}}} {
		m := newMemoryDevice()
		local := t.TempDir()
		r := ApplyPlan(context.Background(), m, p, local, false, syncTestOptions())
		if len(r.Errors) == 0 || len(m.writes) > 0 {
			t.Fatalf("forged plan accepted: errors=%v writes=%v", r.Errors, m.writes)
		}
	}
}
func TestOrdering(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/z.md"] = []byte("z")
	m.files["/ereader/a.md"] = []byte("a")
	m.dirs["/ereader/_metadata"] = true
	local := localBuildFile(t, "wiki/deep/note.md", []byte("new"))
	for i := 0; i < 20; i++ {
		p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, d := range p.Deletes {
			paths = append(paths, d.Path)
		}
		if strings.Join(paths, ",") != "/ereader/a.md,/ereader/z.md,/ereader/_metadata" {
			t.Fatalf("unexpected delete order: %v", paths)
		}
	}
}

func TestDisabledManifestRecoveryWithoutContentChanges(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/_meta/file-hashes.json"] = []byte("broken cache")
	m.files["/ereader/note.md"] = []byte("same")
	local := localBuildFile(t, "note.md", []byte("same"))
	opts := syncTestOptions()
	opts.HashManifest = false
	p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, opts)
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if _, exists := m.files["/ereader/_meta/file-hashes.json"]; exists {
		t.Fatal("disabled manifest did not remove broken cache on unchanged content")
	}
	if _, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions()); err != nil {
		t.Fatal(err)
	}
}
func TestUploadFailureRetainsOldFiles(t *testing.T) {
	for _, failFast := range []bool{true, false} {
		t.Run(fmt.Sprint(failFast), func(t *testing.T) {
			m := newMemoryDevice()
			m.files["/ereader/old.md"] = []byte("old")
			m.files["/ereader/_meta/file-hashes.json"] = []byte(`{"schema":1,"files":{}}`)
			m.uploadErr["/ereader/a.md"] = fmt.Errorf("upload failed")
			local := localBuildFile(t, "a.md", []byte("a"))
			if err := os.WriteFile(filepath.Join(local, "b.md"), []byte("b"), 0644); err != nil {
				t.Fatal(err)
			}
			opts := syncTestOptions()
			opts.FailFast = failFast
			p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
			if err != nil {
				t.Fatal(err)
			}
			r := ApplyPlan(context.Background(), m, p, local, false, opts)
			if len(r.Errors) == 0 {
				t.Fatal("failed upload was not reported")
			}
			if _, ok := m.files["/ereader/old.md"]; !ok {
				t.Fatal("old file deleted after upload failure")
			}
			if _, ok := m.files["/ereader/_meta/file-hashes.json"]; ok {
				t.Fatal("stale receipt survived upload failure")
			}
			wantUploaded := 1
			if failFast {
				wantUploaded = 0
			}
			if r.Uploaded != wantUploaded {
				t.Fatalf("uploaded=%d want=%d", r.Uploaded, wantUploaded)
			}
		})
	}
}
func TestReceiptInvalidationFailureStopsMutations(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/_meta/file-hashes.json"] = []byte(`{"schema":1,"files":{}}`)
	m.deleteErr["/ereader/_meta/file-hashes.json"] = fmt.Errorf("cannot invalidate")
	local := localBuildFile(t, "note.md", []byte("new"))
	opts := syncTestOptions()
	opts.FailFast = false
	p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := ApplyPlan(context.Background(), m, p, local, false, opts)
	if len(r.Errors) == 0 || len(m.writes) != 1 || m.writes[0] != "delete /ereader/_meta/file-hashes.json" {
		t.Fatalf("continued after invalidation failure: errors=%v writes=%v", r.Errors, m.writes)
	}
}
func TestPartialSyncThenLocalRevertUploadsAgain(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "a.md", []byte("old"))
	opts := syncTestOptions()
	ctx := context.Background()
	apply := func() {
		t.Helper()
		p, err := BuildPlan(ctx, m, "/ereader", local, opts)
		if err != nil {
			t.Fatal(err)
		}
		if r := ApplyPlan(ctx, m, p, local, false, opts); len(r.Errors) > 0 {
			t.Fatal(r.Errors)
		}
	}
	apply()
	if err := os.WriteFile(filepath.Join(local, "a.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "z.md"), []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	m.uploadErr["/ereader/z.md"] = fmt.Errorf("later upload fails")
	p, err := BuildPlan(ctx, m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r := ApplyPlan(ctx, m, p, local, false, opts); len(r.Errors) == 0 {
		t.Fatal("expected later upload failure")
	}
	if string(m.files["/ereader/a.md"]) != "new" {
		t.Fatal("first upload did not succeed")
	}
	if err := os.WriteFile(filepath.Join(local, "a.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = BuildPlan(ctx, m, "/ereader", local, opts)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range p.Uploads {
		if op.Path == "/ereader/a.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("old receipt incorrectly skipped reverted local file")
	}
}
func TestManifestReadFailureIsNotAbsence(t *testing.T) {
	m := newMemoryDevice()
	m.readErr["/ereader/_meta/file-hashes.json"] = fmt.Errorf("network failure")
	if _, err := LoadRemoteHashManifest(context.Background(), m, "/ereader"); err == nil {
		t.Fatal("manifest read failure hidden")
	}
}
func TestRemoteCompareFailureStopsPlan(t *testing.T) {
	m := newMemoryDevice()
	m.files["/ereader/note.md"] = []byte("old")
	m.readErr["/ereader/note.md"] = fmt.Errorf("network failure")
	local := localBuildFile(t, "note.md", []byte("new"))
	opts := syncTestOptions()
	opts.HashManifest = false
	if _, err := BuildPlan(context.Background(), m, "/ereader", local, opts); err == nil {
		t.Fatal("remote comparison failure hidden")
	}
}
func TestInitOwnershipAndRefusal(t *testing.T) {
	for _, marker := range []string{"broken", `{"schema":2,"tool":"ereader","root":"/ereader"}`, `{"schema":1,"tool":"other","root":"/ereader"}`, `{"schema":1,"tool":"ereader","root":"/other"}`} {
		m := newMemoryDevice()
		m.files["/ereader/_meta/ownership.json"] = []byte(marker)
		if err := DeviceInit(context.Background(), m, "/ereader", "ereader"); err == nil || len(m.writes) > 0 {
			t.Fatalf("invalid marker changed device: marker=%s writes=%v", marker, m.writes)
		}
	}
	m := newMemoryDevice()
	if err := DeviceInit(context.Background(), m, "/ereader", "ereader"); err != nil || len(m.writes) > 0 {
		t.Fatalf("valid initialization was not idempotent: err=%v writes=%v", err, m.writes)
	}
	delete(m.files, "/ereader/_meta/ownership.json")
	m.files["/ereader/unowned.md"] = []byte("keep")
	if err := DeviceInit(context.Background(), m, "/ereader", "ereader"); err == nil || len(m.writes) > 0 {
		t.Fatal("nonempty unowned root claimed")
	}
}
func TestApplyRejectsRootMismatchAndRemovedUploads(t *testing.T) {
	for _, changeRoot := range []bool{true, false} {
		m := newMemoryDevice()
		local := localBuildFile(t, "note.md", []byte("new"))
		opts := syncTestOptions()
		p, err := BuildPlan(context.Background(), m, "/ereader", local, opts)
		if err != nil {
			t.Fatal(err)
		}
		if changeRoot {
			opts.DeviceRoot = "/other"
		} else {
			p.Uploads = nil
		}
		r := ApplyPlan(context.Background(), m, p, local, false, opts)
		if len(r.Errors) == 0 || len(m.writes) > 0 {
			t.Fatal("mismatched or incomplete plan accepted")
		}
	}
}
func TestSyncRejectsInvalidRoots(t *testing.T) {
	for _, root := range []string{"/", "/.", "/a/..", "relative", "/a//b", "/a\\b"} {
		m := newMemoryDevice()
		local := t.TempDir()
		opts := syncTestOptions()
		opts.DeviceRoot = root
		if err := DeviceInit(context.Background(), m, root, "ereader"); err == nil {
			t.Fatalf("init accepted root %q", root)
		}
		if _, err := BuildPlan(context.Background(), m, root, local, opts); err == nil {
			t.Fatalf("plan accepted root %q", root)
		}
		if len(m.writes) > 0 {
			t.Fatalf("mutated invalid root %q", root)
		}
	}
}

func TestManifestDoesNotSkipChangedRemoteSize(t *testing.T) {
	m := newMemoryDevice()
	local := localBuildFile(t, "note.md", []byte("complete"))
	hashes, err := HashLocalTree(local)
	if err != nil {
		t.Fatal(err)
	}
	data, err := (&HashManifest{Files: hashes}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	m.files["/ereader/_meta/file-hashes.json"] = data
	m.files["/ereader/note.md"] = []byte("short")
	p, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Uploads) != 1 {
		t.Fatal("truncated remote file was skipped")
	}
}
func TestHashLocalTreeRejectsSpecialFiles(t *testing.T) {
	local := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(local, "socket"))
	if err != nil {
		t.Skipf("Unix sockets unavailable: %v", err)
	}
	defer listener.Close()
	if _, err := HashLocalTree(local); err == nil {
		t.Fatal("socket accepted as local file")
	}
}
func TestHashLocalTreeRejectsDirectorySymlinks(t *testing.T) {
	target := t.TempDir()
	local := t.TempDir()
	link := filepath.Join(local, "directory")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := HashLocalTree(local); err == nil {
		t.Fatal("directory symlink in build accepted")
	}
	for _, root := range []string{link, link + string(os.PathSeparator)} {
		if _, err := HashLocalTree(root); err == nil {
			t.Fatal("symlink used as build root accepted")
		}
	}
}
func TestInvalidRemoteListingShape(t *testing.T) {
	for _, entries := range [][]FileEntry{{{Name: "note.md"}, {Name: "note.md"}}, {{Name: "note.md", Size: -1}}} {
		m := newMemoryDevice()
		m.extra["/ereader"] = entries
		local := localBuildFile(t, "note.md", []byte("new"))
		if _, err := BuildPlan(context.Background(), m, "/ereader", local, syncTestOptions()); err == nil {
			t.Fatal("invalid remote listing accepted")
		}
	}
}
