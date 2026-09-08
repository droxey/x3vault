package config

import (
	"path/filepath"
	"testing"
)

func TestWikiDirsAllExceptIgnoredIncludesCustom(t *testing.T) {
	d := DefaultWikiDirs()
	if !d.ShouldIncludeRelPath("domains/inbox/entities/foo.md") {
		t.Fatal("expected custom dir to be included in all_except_ignored mode")
	}
	if !d.ShouldIncludeRelPath("synthesis/open-questions.md") {
		t.Fatal("expected synthesis to be included")
	}
}

func TestWikiDirsAllExceptIgnoredSkipsIgnored(t *testing.T) {
	d := DefaultWikiDirs()
	if d.ShouldIncludeRelPath("script/readme.md") {
		t.Fatal("expected script/ to be ignored")
	}
	if d.ShouldWalkDir("references") {
		t.Fatal("expected references/ dir to be skipped")
	}
}

func TestWikiDirsRootMarkdownAlwaysIncluded(t *testing.T) {
	d := DefaultWikiDirs()
	for _, rel := range []string{"index.md", "log.md", "custom-hub.md"} {
		if !d.ShouldIncludeRelPath(rel) {
			t.Fatalf("expected root md %q to be included", rel)
		}
	}
}

func TestWikiDirsWhitelistMode(t *testing.T) {
	d := WikiDirs{
		Mode:    WikiModeWhitelist,
		Allowed: []string{"entities"},
		Ignored: []string{"script"},
	}
	if !d.ShouldIncludeRelPath("entities/foo.md") {
		t.Fatal("expected entities in whitelist")
	}
	if d.ShouldIncludeRelPath("concepts/foo.md") {
		t.Fatal("expected concepts excluded in whitelist")
	}
}

func TestWikiDirsRestoreDefaults(t *testing.T) {
	d := DefaultWikiDirs()
	d.AddIgnored("drafts")
	d.RestoreDefaults()
	if d.Mode != WikiModeAllExceptIgnored {
		t.Fatalf("mode = %q", d.Mode)
	}
	if len(d.Ignored) != len(LLMWikiDefaults.Ignored) {
		t.Fatalf("ignored len = %d", len(d.Ignored))
	}
}

func TestWikiDirsValidateOverlap(t *testing.T) {
	d := WikiDirs{
		Mode:    WikiModeWhitelist,
		Allowed: []string{"script"},
		Ignored: []string{"script"},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected overlap validation error")
	}
}

func TestShouldWalkDirAncestorWhitelist(t *testing.T) {
	d := WikiDirs{
		Mode:    WikiModeWhitelist,
		Allowed: []string{"domains/inbox/entities"},
	}
	if !d.ShouldWalkDir("domains") {
		t.Fatal("expected ancestor dir domains to be walked in whitelist mode")
	}
	if d.ShouldWalkDir("concepts") {
		t.Fatal("expected unrelated dir concepts to be skipped")
	}
}

func TestShouldWalkDirNativeNestedPaths(t *testing.T) {
	dirs := WikiDirs{Mode: WikiModeWhitelist, Allowed: []string{"domains/inbox/entities"}, Ignored: []string{"domains/inbox/entities/private"}}
	for _, parts := range [][]string{{"domains"}, {"domains", "inbox"}, {"domains", "inbox", "entities"}, {"domains", "inbox", "entities", "public"}} {
		native := filepath.Join(parts...)
		if !dirs.ShouldWalkDir(native) {
			t.Errorf("should walk native directory %q", native)
		}
	}
	if dirs.ShouldWalkDir(filepath.Join("domains", "inbox", "entities", "private")) {
		t.Fatal("walked ignored native directory")
	}
	if dirs.ShouldWalkDir(filepath.Join("domains", "other")) {
		t.Fatal("walked unrelated native directory")
	}
}
