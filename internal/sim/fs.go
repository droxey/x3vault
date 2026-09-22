package sim

import (
	"fmt"
	"path"
	"strings"
	"sync"
)

// Store is an in-memory POSIX file tree used by the Witch simulator.
type Store struct {
	mu    sync.Mutex
	dirs  map[string]bool
	files map[string][]byte
}

func NewStore() *Store {
	return &Store{
		dirs:  map[string]bool{"/": true},
		files: map[string][]byte{},
	}
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "." {
		return "/"
	}
	return p
}

func parentOf(p string) string {
	p = cleanPath(p)
	if p == "/" {
		return "/"
	}
	parent := path.Dir(p)
	if parent == "" {
		return "/"
	}
	return parent
}

func (s *Store) HasDir(p string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirs[cleanPath(p)]
}

func (s *Store) ReadFile(p string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[cleanPath(p)]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, true
}

type Entry struct {
	Name        string
	Size        int64
	IsDirectory bool
}

func (s *Store) List(dir string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir = cleanPath(dir)
	if !s.dirs[dir] {
		return nil, fmt.Errorf("not found")
	}

	prefix := dir
	if prefix != "/" {
		prefix += "/"
	} else {
		prefix = "/"
	}

	seen := map[string]bool{}
	var entries []Entry

	consider := func(full string, isDir bool, size int64) {
		if full == dir {
			return
		}
		if !strings.HasPrefix(full, prefix) {
			return
		}
		rest := strings.TrimPrefix(full, prefix)
		if rest == "" {
			return
		}
		name := rest
		nested := false
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name = rest[:i]
			nested = true
		}
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		if nested || isDir && strings.Contains(rest, "/") {
			entries = append(entries, Entry{Name: name, IsDirectory: true})
			return
		}
		if isDir {
			entries = append(entries, Entry{Name: name, IsDirectory: true})
			return
		}
		entries = append(entries, Entry{Name: name, Size: size})
	}

	for p, data := range s.files {
		consider(p, false, int64(len(data)))
	}
	for d := range s.dirs {
		consider(d, true, 0)
	}
	return entries, nil
}

func (s *Store) Mkdir(parent, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	parent = cleanPath(parent)
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("invalid name")
	}
	if !s.dirs[parent] {
		return fmt.Errorf("missing parent")
	}
	p := path.Join(parent, name)
	if s.dirs[p] || fileExistsLocked(s, p) {
		return fmt.Errorf("already exists")
	}
	s.dirs[p] = true
	return nil
}

func (s *Store) Upload(dir, filename string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir = cleanPath(dir)
	filename = path.Base(filename)
	if filename == "." || filename == "/" || filename == "" {
		return fmt.Errorf("invalid filename")
	}
	p := path.Join(dir, filename)
	if !s.dirs[dir] {
		return fmt.Errorf("missing parent")
	}
	if s.dirs[p] {
		return fmt.Errorf("conflicting directory")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	s.files[p] = cp
	return nil
}

func (s *Store) Delete(itemPath, itemType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	itemPath = cleanPath(itemPath)
	if itemPath == "/" {
		return fmt.Errorf("refuse root")
	}
	if itemType == "directory" || itemType == "folder" {
		if !s.dirs[itemPath] {
			return fmt.Errorf("not found")
		}
		prefix := itemPath + "/"
		for d := range s.dirs {
			if strings.HasPrefix(d, prefix) {
				return fmt.Errorf("directory not empty")
			}
		}
		for f := range s.files {
			if strings.HasPrefix(f, prefix) {
				return fmt.Errorf("directory not empty")
			}
		}
		delete(s.dirs, itemPath)
		return nil
	}
	if _, ok := s.files[itemPath]; !ok {
		return fmt.Errorf("not found")
	}
	delete(s.files, itemPath)
	return nil
}

func fileExistsLocked(s *Store, p string) bool {
	_, ok := s.files[p]
	return ok
}

func (s *Store) Snapshot() (dirs []string, files map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dirs = make([]string, 0, len(s.dirs))
	for d := range s.dirs {
		dirs = append(dirs, d)
	}
	files = make(map[string]int, len(s.files))
	for p, data := range s.files {
		files[p] = len(data)
	}
	return dirs, files
}
