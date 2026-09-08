package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/droxey/x3vault/internal/config"
)

// mockDevice implements a minimal Witch Reader HTTP API for tests.
type mockDevice struct {
	mu      sync.Mutex
	dirs    map[string]bool
	files   map[string][]byte
	uploads []string
}

func newMockDevice() *mockDevice {
	return &mockDevice{
		dirs:  map[string]bool{"/": true, "/ereader": true},
		files: map[string][]byte{},
	}
}

func (m *mockDevice) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/status":
		_ = json.NewEncoder(w).Encode(Status{Version: "test", Device: "X3", IP: "127.0.0.1", Mode: "transfer"})
	case r.Method == http.MethodGet && r.URL.Path == "/api/files":
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		if !m.dirs[path] {
			http.NotFound(w, r)
			return
		}
		entries := []FileEntry{}
		prefix := strings.TrimSuffix(path, "/") + "/"
		seen := map[string]bool{}
		for p, data := range m.files {
			if !strings.HasPrefix(p, prefix) || p == path {
				continue
			}
			rest := strings.TrimPrefix(p, prefix)
			name := rest
			if i := strings.Index(rest, "/"); i >= 0 {
				name = rest[:i]
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			if strings.Contains(rest, "/") {
				entries = append(entries, FileEntry{Name: name, IsDirectory: true})
			} else {
				entries = append(entries, FileEntry{Name: name, Size: int64(len(data))})
			}
		}
		for d := range m.dirs {
			if d == path {
				continue
			}
			if strings.HasPrefix(d, prefix) {
				rest := strings.TrimPrefix(d, prefix)
				name := rest
				if i := strings.Index(rest, "/"); i >= 0 {
					name = rest[:i]
				}
				if name != "" && !seen[name] {
					seen[name] = true
					entries = append(entries, FileEntry{Name: name, IsDirectory: true})
				}
			}
		}
		_ = json.NewEncoder(w).Encode(entries)
	case r.Method == http.MethodPost && r.URL.Path == "/mkdir":
		_ = r.ParseForm()
		parent := r.Form.Get("path")
		name := r.Form.Get("name")
		if parent == "" {
			parent = "/"
		}
		p := path.Join(parent, name)
		if !m.dirs[parent] {
			http.Error(w, "missing parent", http.StatusNotFound)
			return
		}
		if _, exists := m.files[p]; exists || m.dirs[p] {
			http.Error(w, "already exists", http.StatusConflict)
			return
		}
		m.dirs[p] = true
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/upload":
		dir := r.URL.Query().Get("path")
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		_ = file.Close()
		p := path.Join(dir, header.Filename)
		if !m.dirs[dir] || m.dirs[p] {
			http.Error(w, "missing parent or conflicting directory", http.StatusConflict)
			return
		}
		m.files[p] = data
		m.uploads = append(m.uploads, p)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/delete":
		_ = r.ParseForm()
		p := r.Form.Get("path")
		if r.Form.Get("type") == "directory" {
			for child := range m.dirs {
				if strings.HasPrefix(child, p+"/") {
					http.Error(w, "directory not empty", http.StatusConflict)
					return
				}
			}
			for child := range m.files {
				if strings.HasPrefix(child, p+"/") {
					http.Error(w, "directory not empty", http.StatusConflict)
					return
				}
			}
			delete(m.dirs, p)
		} else {
			delete(m.files, p)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/download":
		p := r.URL.Query().Get("path")
		data, ok := m.files[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func TestDeviceInitAndSyncPlan(t *testing.T) {
	dev := newMockDevice()
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()

	ctx := context.Background()
	tr := NewTransport(srv.URL, 5*time.Second)
	if err := DeviceInit(ctx, tr, "/ereader", "ereader"); err != nil {
		t.Fatalf("DeviceInit: %v", err)
	}
	if !dev.dirs["/ereader/_meta"] {
		t.Fatal("expected /ereader/_meta dir")
	}
	if _, ok := dev.files["/ereader/_meta/ownership.json"]; !ok {
		t.Fatal("expected ownership.json uploaded")
	}
	owned, err := HasOwnership(ctx, tr, "/ereader")
	if err != nil {
		t.Fatalf("HasOwnership: %v", err)
	}
	if !owned {
		t.Fatal("expected ownership after init")
	}

	local := t.TempDir()
	wiki := filepath.Join(local, "wiki")
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		DeviceRoot:     "/ereader",
		FailFast:       true,
		HashManifest:   true,
		CleanEmptyDirs: true,
		OwnershipTool:  config.DefaultOwnershipTool,
		Progress:       io.Discard,
	}
	plan, err := BuildPlan(ctx, tr, opts.DeviceRoot, local, opts)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Uploads) == 0 {
		t.Fatal("expected uploads in plan")
	}

	res := ApplyPlan(ctx, tr, plan, local, false, opts)
	if len(res.Errors) != 0 {
		t.Fatalf("ApplyPlan errors: %v", res.Errors)
	}
	if res.Uploaded == 0 {
		t.Fatal("expected uploads applied")
	}
}

func TestHasOwnershipReturnsReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer srv.Close()
	tr := NewTransport(srv.URL, time.Second)
	_, err := HasOwnership(context.Background(), tr, "/ereader")
	if err == nil {
		t.Fatal("expected error when marker read fails")
	}
}

func TestHTTPDeviceSyncFinalStateAndIdempotence(t *testing.T) {
	dev := newMockDevice()
	srv := httptest.NewServer(http.HandlerFunc(dev.handler))
	defer srv.Close()
	ctx := context.Background()
	transport := NewTransport(srv.URL, time.Second)
	opts := syncTestOptions()
	if err := DeviceInit(ctx, transport, opts.DeviceRoot, opts.OwnershipTool); err != nil {
		t.Fatal(err)
	}
	dev.mu.Lock()
	dev.dirs["/ereader/old"] = true
	dev.dirs["/ereader/_metadata"] = true
	dev.files["/ereader/old/obsolete.md"] = []byte("obsolete")
	dev.files["/ereader/_meta/keep.json"] = []byte("preserve")
	dev.mu.Unlock()
	contents := bytes.Repeat([]byte("markdown bytes\n"), 7000)
	local := localBuildFile(t, "wiki/deep/note # ? &.md", contents)
	plan, err := BuildPlan(ctx, transport, opts.DeviceRoot, local, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result := ApplyPlan(ctx, transport, plan, local, false, opts); len(result.Errors) > 0 {
		t.Fatal(result.Errors)
	}
	data, err := transport.ReadFile(ctx, "/ereader/wiki/deep/note # ? &.md")
	if err != nil || !bytes.Equal(data, contents) {
		t.Fatalf("uploaded bytes do not match: len=%d err=%v", len(data), err)
	}
	dev.mu.Lock()
	oldDir, oldFile, metaDir := dev.dirs["/ereader/old"], dev.files["/ereader/old/obsolete.md"], dev.dirs["/ereader/_metadata"]
	preserved := string(dev.files["/ereader/_meta/keep.json"])
	dev.mu.Unlock()
	if oldDir || oldFile != nil || metaDir || preserved != "preserve" {
		t.Fatal("sync final state did not preserve metadata and remove obsolete content")
	}
	plan, err = BuildPlan(ctx, transport, opts.DeviceRoot, local, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Uploads)+len(plan.Mkdirs)+len(plan.Deletes) != 0 {
		t.Fatalf("unchanged second plan performs content operations: %+v", plan)
	}
	if result := ApplyPlan(ctx, transport, plan, local, false, opts); len(result.Errors) > 0 {
		t.Fatal(result.Errors)
	}
	manifest, err := LoadRemoteHashManifest(ctx, transport, opts.DeviceRoot)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := HashLocalTree(local)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(manifest.Files, hashes) {
		t.Fatal("receipt does not match final content")
	}
}
