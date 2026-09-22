package sim

import (
	"errors"
	"path"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrMissingParent = errors.New("missing parent")
	ErrDirNotEmpty   = errors.New("directory not empty")
	ErrInvalidPath   = errors.New("invalid path")
	ErrInvalidName   = errors.New("invalid name")
	ErrConflict      = errors.New("conflicting directory")
	ErrRefuseRoot    = errors.New("refuse root")
)

const maxPathBytes = 1024

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
	c, err := parseDevicePath(p)
	if err != nil {
		return "/"
	}
	return c
}

// parseDevicePath validates and canonicalizes a Witch POSIX path.
func parseDevicePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) || strings.Contains(p, "\\") {
		return "", ErrInvalidPath
	}
	if !utf8.ValidString(p) {
		return "", ErrInvalidPath
	}
	if len(p) > maxPathBytes {
		return "", ErrInvalidPath
	}
	if p == "" {
		return "/", nil
	}
	c := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if c == "." {
		c = "/"
	}
	if !path.IsAbs(c) {
		return "", ErrInvalidPath
	}
	return c, nil
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
	c, err := parseDevicePath(p)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirs[c]
}

func (s *Store) ReadFile(p string) ([]byte, bool) {
	c, err := parseDevicePath(p)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[c]
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

	var err error
	dir, err = parseDevicePath(dir)
	if err != nil {
		return nil, err
	}
	if !s.dirs[dir] {
		return nil, ErrNotFound
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

	var err error
	parent, err = parseDevicePath(parent)
	if err != nil {
		return err
	}
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") || strings.ContainsRune(name, 0) || name == "." || name == ".." {
		return ErrInvalidName
	}
	if !s.dirs[parent] {
		return ErrMissingParent
	}
	p := path.Join(parent, name)
	if s.dirs[p] || fileExistsLocked(s, p) {
		return ErrAlreadyExists
	}
	s.dirs[p] = true
	return nil
}

func (s *Store) Upload(dir, filename string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	dir, err = parseDevicePath(dir)
	if err != nil {
		return err
	}
	filename = path.Base(filename)
	if filename == "." || filename == "/" || filename == "" || filename == ".." || strings.ContainsRune(filename, 0) {
		return ErrInvalidName
	}
	p := path.Join(dir, filename)
	if !s.dirs[dir] {
		return ErrMissingParent
	}
	if s.dirs[p] {
		return ErrConflict
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	s.files[p] = cp
	return nil
}

func (s *Store) Delete(itemPath, itemType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	itemPath, err = parseDevicePath(itemPath)
	if err != nil {
		return err
	}
	if itemPath == "/" {
		return ErrRefuseRoot
	}
	if itemType == "directory" || itemType == "folder" {
		if !s.dirs[itemPath] {
			return ErrNotFound
		}
		prefix := itemPath + "/"
		for d := range s.dirs {
			if strings.HasPrefix(d, prefix) {
				return ErrDirNotEmpty
			}
		}
		for f := range s.files {
			if strings.HasPrefix(f, prefix) {
				return ErrDirNotEmpty
			}
		}
		delete(s.dirs, itemPath)
		return nil
	}
	if _, ok := s.files[itemPath]; !ok {
		return ErrNotFound
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
