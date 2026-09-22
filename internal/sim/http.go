package sim

import (
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

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
	return mux
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
	_ = r.ParseForm()
	parent := r.Form.Get("path")
	name := r.Form.Get("name")
	if err := s.Store.Mkdir(parent, name); err != nil {
		if err.Error() == "already exists" {
			http.Error(w, "already exists", http.StatusConflict)
			return
		}
		if err.Error() == "missing parent" {
			http.Error(w, "missing parent", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	_ = r.ParseForm()
	p := r.Form.Get("path")
	typ := r.Form.Get("type")
	if err := s.Store.Delete(p, typ); err != nil {
		msg := err.Error()
		if msg == "directory not empty" {
			http.Error(w, "Folder is not empty. Delete contents first. directory not empty", http.StatusConflict)
			return
		}
		if msg == "not found" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, msg, http.StatusBadRequest)
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
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
