package markdown

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// NoteReader anchors reads to a source directory opened before build callbacks.
// Renaming or replacing that pathname does not change the directory being read.
// It rejects symbolic links and non-regular entries; this is a read boundary,
// not a general filesystem sandbox for concurrent hostile mutations.
type NoteReader struct {
	root   *os.Root
	source string
}

func OpenNoteReader(sourceRoot string) (*NoteReader, error) {
	if strings.TrimSpace(sourceRoot) == "" {
		return nil, fmt.Errorf("note source root must not be empty")
	}
	source, err := filepath.Abs(sourceRoot)
	if err != nil {
		return nil, err
	}
	expected, err := os.Lstat(source)
	if err != nil {
		return nil, fmt.Errorf("inspect note source: %w", err)
	}
	if !expected.IsDir() || expected.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("note source must be a directory without symlinks")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, fmt.Errorf("open note source: %w", err)
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, actual) {
		closeErr := root.Close()
		if err == nil {
			err = fmt.Errorf("note source changed while opening")
		}
		return nil, errors.Join(err, closeErr)
	}
	return &NoteReader{root: root, source: source}, nil
}

func (r *NoteReader) Close() error { return r.root.Close() }

// Read takes a source-relative path, never an absolute path obtained from a
// discovered entry. The root handle also prevents a replaced link from escaping
// the source between inspection and opening.
func (r *NoteReader) Read(ctx context.Context, relPath string) ([]byte, error) {
	if r == nil || r.root == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	local := filepath.FromSlash(relPath)
	if !filepath.IsLocal(local) || strings.Contains(relPath, `\`) {
		return nil, fmt.Errorf("invalid source-relative note path %q", relPath)
	}
	parts := strings.Split(relPath, "/")
	var expected os.FileInfo
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("invalid source-relative note path %q", relPath)
		}
		prefix := filepath.Join(parts[:i+1]...)
		info, err := r.root.Lstat(prefix)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("note path contains a symlink: %s", relPath)
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return nil, fmt.Errorf("note parent is not a directory: %s", relPath)
			}
		} else {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("note is not a regular file: %s", relPath)
			}
			expected = info
		}
	}
	in, err := r.root.Open(local)
	if err != nil {
		return nil, err
	}
	actual, statErr := in.Stat()
	if statErr != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		closeErr := in.Close()
		if statErr == nil {
			statErr = fmt.Errorf("note changed while opening: %s", relPath)
		}
		return nil, errors.Join(statErr, closeErr)
	}
	data, readErr := io.ReadAll(&contextReader{ctx: ctx, r: in})
	return data, errors.Join(readErr, in.Close())
}

func (r *NoteReader) readReference(ctx context.Context, absPath, relPath string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	absolute, err := filepath.Abs(absPath)
	if err != nil {
		return nil, err
	}
	expected := filepath.Join(r.source, filepath.FromSlash(relPath))
	if absolute != expected {
		return nil, fmt.Errorf("note reference does not match source: %s", relPath)
	}
	return r.Read(ctx, relPath)
}

func readNote(ctx context.Context, absPath, relPath string, opts NormalizeOpts) ([]byte, error) {
	if opts.NoteReader != nil {
		return opts.NoteReader.readReference(ctx, absPath, relPath)
	}
	reader, err := OpenNoteReader(opts.SourceRoot)
	if err != nil {
		return nil, err
	}
	data, readErr := reader.readReference(ctx, absPath, relPath)
	return data, errors.Join(readErr, reader.Close())
}
