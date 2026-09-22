package sim

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

const maxUploadBytes = 16 << 20

// Status matches internal/sync.Status JSON the CLI decodes.
type Status struct {
	Version  string `json:"version"`
	Device   string `json:"device"`
	IP       string `json:"ip"`
	Mode     string `json:"mode"`
	RSSI     int    `json:"rssi"`
	FreeHeap int    `json:"freeHeap"`
	Uptime   int    `json:"uptime"`
	SDReady  bool   `json:"sdReady"`
}

// FileEntry matches internal/sync.FileEntry JSON.
type FileEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	IsDirectory bool   `json:"isDirectory"`
	IsEpub      bool   `json:"isEpub"`
}

// Server is a Witch Reader file-transfer stand-in.
type Server struct {
	Store   *Store
	Started time.Time
	Version string
}

func NewServer() *Server {
	return &Server{
		Store:   NewStore(),
		Started: time.Now(),
		Version: "sim-0.1.0",
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/files", s.handleFiles)
	mux.HandleFunc("/mkdir", s.handleMkdir)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/download", s.handleDownload)
	mux.HandleFunc("/", s.handleUI)
	return recoverHandler(mux)
}

func recoverHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := Status{
		Version:  s.Version,
		Device:   "X3",
		IP:       "127.0.0.1",
		Mode:     "transfer",
		RSSI:     -40,
		FreeHeap: 120000,
		Uptime:   int(time.Since(s.Started).Seconds()),
		SDReady:  true,
	}
	writeJSON(w, st)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := r.URL.Query().Get("path")
	entries, err := s.Store.List(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FileEntry{
			Name:        e.Name,
			Size:        e.Size,
			IsDirectory: e.IsDirectory,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	parent := r.Form.Get("path")
	name := r.Form.Get("name")
	if err := s.Store.Mkdir(parent, name); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := r.URL.Query().Get("path")
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	if int64(len(data)) > maxUploadBytes {
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.Store.Upload(dir, header.Filename, data); err != nil {
		http.Error(w, "missing parent or conflicting directory", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	p := r.Form.Get("path")
	typ := r.Form.Get("type")
	if err := s.Store.Delete(p, typ); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Query().Get("path")
	data, ok := s.Store.ReadFile(p)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, "already exists", http.StatusConflict)
	case errors.Is(err, ErrMissingParent):
		http.Error(w, "missing parent", http.StatusNotFound)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrDirNotEmpty):
		http.Error(w, "Folder is not empty. Delete contents first. directory not empty", http.StatusConflict)
	case errors.Is(err, ErrConflict):
		http.Error(w, "missing parent or conflicting directory", http.StatusConflict)
	case errors.Is(err, ErrInvalidPath), errors.Is(err, ErrInvalidName), errors.Is(err, ErrRefuseRoot):
		http.Error(w, "invalid path", http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusBadRequest)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func joinURLPath(dir, name string) string {
	if dir == "" || dir == "/" {
		return path.Join("/", name)
	}
	return path.Join(dir, name)
}

func isMarkdown(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}
