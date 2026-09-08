package vault

import (
	"fmt"
	"github.com/droxey/x3vault/internal/pathutil"
)

// AssertBuildWritePath ensures writes stay under the canonical build root,
// including when a not-yet-created destination has a symlinked ancestor.
func AssertBuildWritePath(path, buildRoot string) error {
	pathAbs, err := pathutil.Canonical(path)
	if err != nil {
		return fmt.Errorf("resolve write path: %w", err)
	}
	rootAbs, err := pathutil.Canonical(buildRoot)
	if err != nil {
		return fmt.Errorf("resolve build root: %w", err)
	}
	if !pathutil.ContainedIn(pathAbs, rootAbs) {
		return fmt.Errorf("refusing to write outside build output %s: %s", buildRoot, path)
	}
	return nil
}
