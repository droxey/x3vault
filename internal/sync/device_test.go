package sync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/droxey/x3vault/internal/config"
)

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
		var entries []FileEntry
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
		m.dirs[parent+"/"+name] = true
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/upload"):
		path := r.URL.Query().Get("path")
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		_ = file.Close()
		filename := filepath.Base(r.URL.Query().Get("name"))
		if filename == "." {
			filename = "upload"
		}
		for k := range r.MultipartForm.File {
			if len(r.MultipartForm.File[k]) > 0 {
				filename = r.MultipartForm.File[k][0].Filename
			}
		}
		full := strings.TrimSuffix(path, "/") + "/" + filename
		m.files[full] = data
		m.uploads = append(m.uploads, full)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/delete":
		_ = r.ParseForm()
		p := r.Form.Get("path")
		if r.Form.Get("type") == "directory" {
			delete(m.dirs, p)
		} else {
			delete(m.files, p)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/download":
		path := r.URL.Query().Get("path")
		data, ok := m.files[path]
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
	owned, err := HasOwnership(ctx, tr, "/ereader")
	if err != nil {
		t.Fatalf("HasOwnership: %v", err)
	}
	if !owned {
		t.Fatal("expected ownership after init")
	}

	local := t.TempDir()
	wikiDir := filepath.Join(local, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "index.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	opts := OptionsFromConfig(cfg)
	opts.DeviceRoot = "/ereader"
	opts.Progress = io.Discard

	plan, err := BuildPlan(ctx, tr, opts.DeviceRoot, local, opts)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Uploads) == 0 {
		t.Fatal("expected uploads in plan")
	}

	res := ApplyPlan(ctx, tr, plan, local, false, opts)
	if len(res.Errors) > 0 {
		t.Fatalf("ApplyPlan errors: %v", res.Errors)
	}
	if res.Uploaded == 0 {
		t.Fatal("expected uploads applied")
	}
}

func TestHasOwnershipReturnsListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer srv.Close()
	tr := NewTransport(srv.URL, time.Second)
	_, err := HasOwnership(context.Background(), tr, "/ereader")
	if err == nil {
		t.Fatal("expected error when list fails")
	}
}
