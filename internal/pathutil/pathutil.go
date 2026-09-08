package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ContainedIn reports whether child is equal to or nested under parent.
// It checks lexical containment; use Canonical first when symlinks matter.
func ContainedIn(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Canonical returns an absolute path with existing symlink ancestors resolved.
// Missing final components are appended to the nearest existing ancestor. Errors
// other than absence (including dangling symlinks) are never treated as absence.
func Canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := abs
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", fmt.Errorf("resolve %s: %w", current, err)
			}
			if len(suffix) > 0 {
				info, err := os.Stat(resolved)
				if err != nil {
					return "", err
				}
				if !info.IsDir() {
					return "", fmt.Errorf("path ancestor is not a directory: %s", current)
				}
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

// ValidateRelative accepts portable, slash-separated relative path segments.
// Validation deliberately precedes any cleaning that could hide traversal.
func ValidateRelative(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("must be a non-empty relative path")
	}
	if strings.ContainsAny(path, `\:<>"|?*`) || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return fmt.Errorf("contains non-portable path characters")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("must not contain empty, dot, or parent path segments")
		}
		if strings.TrimSpace(segment) != segment || strings.HasSuffix(segment, ".") {
			return fmt.Errorf("contains a non-portable path segment %q", segment)
		}
	}
	return nil
}
