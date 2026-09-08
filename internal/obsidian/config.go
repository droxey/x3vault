package obsidian

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type appJSON struct {
	AttachmentFolderPath string `json:"attachmentFolderPath"`
}

// AttachmentFolder returns Obsidian's configured attachment directory relative
// to the vault root, or "" when attachments live beside each note.
func AttachmentFolder(vaultRoot string) string {
	path := filepath.Join(vaultRoot, ".obsidian", "app.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg appJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	p := strings.TrimSpace(cfg.AttachmentFolderPath)
	if p == "" || p == "." || p == "./" {
		return ""
	}
	p = filepath.ToSlash(p)
	p = strings.Trim(p, "/")
	return p
}

// ResolveAttachmentPath maps an Obsidian attachmentFolderPath value to an
// absolute directory under vaultRoot.
func ResolveAttachmentPath(vaultRoot, attachmentRel string) string {
	if attachmentRel == "" {
		return ""
	}
	if filepath.IsAbs(attachmentRel) {
		return attachmentRel
	}
	return filepath.Join(vaultRoot, filepath.FromSlash(attachmentRel))
}
