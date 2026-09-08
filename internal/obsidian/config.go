package obsidian

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/droxey/x3vault/internal/pathutil"
)

type appJSON struct {
	AttachmentFolderPath string `json:"attachmentFolderPath"`
}

// AttachmentFolder returns Obsidian's configured attachment directory relative
// to the vault root. A leading ./ denotes a directory relative to each note.
func AttachmentFolder(vaultRoot string) string {
	// A vault alias is supported, but metadata entries themselves must be real
	// directories/files. Rooted reads also prevent an entry swap escaping the vault.
	vault, err := os.OpenRoot(vaultRoot)
	if err != nil {
		return ""
	}
	defer vault.Close()
	dirInfo, err := vault.Lstat(".obsidian")
	if err != nil || !dirInfo.IsDir() {
		return ""
	}
	metadata, err := vault.OpenRoot(".obsidian")
	if err != nil {
		return ""
	}
	defer metadata.Close()
	openedDir, err := metadata.Stat(".")
	if err != nil || !os.SameFile(dirInfo, openedDir) {
		return ""
	}
	appInfo, err := metadata.Lstat("app.json")
	if err != nil || !appInfo.Mode().IsRegular() {
		return ""
	}
	file, err := metadata.Open("app.json")
	if err != nil {
		return ""
	}
	defer file.Close()
	openedFile, err := file.Stat()
	if err != nil || !openedFile.Mode().IsRegular() || !os.SameFile(appInfo, openedFile) {
		return ""
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return ""
	}
	var cfg appJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	p := strings.TrimSpace(cfg.AttachmentFolderPath)
	if p == "./" {
		return "."
	}
	if p == "" || p == "." {
		return p
	}
	check := strings.TrimPrefix(p, "./")
	if err := pathutil.ValidateRelative(check); err != nil {
		return ""
	}
	return p
}

// ResolveAttachmentPath maps a validated relative folder to its base directory.
// Callers must select the vault or note directory and enforce read containment.
func ResolveAttachmentPath(base, attachmentRel string) string {
	if attachmentRel == "" {
		return ""
	}
	if attachmentRel == "." {
		return base
	}
	if err := pathutil.ValidateRelative(attachmentRel); err != nil {
		return ""
	}
	return filepath.Join(base, filepath.FromSlash(attachmentRel))
}
