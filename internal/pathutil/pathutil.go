package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ContainedIn reports whether child is equal to or nested under parent.
func ContainedIn(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(child, parent+sep)
}
