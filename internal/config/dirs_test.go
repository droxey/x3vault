package config

import (
	"testing"
)

func TestWikiDirsShouldIncludeRelPath(t *testing.T) {
	d := DefaultWikiDirs()

	cases := []struct {
		rel    string
		want   bool
		reason string
	}{
		{"index.md", true, "root hub pages"},
		{"log.md", true, "root log"},
		{"entities/foo.md", true, "default allowed"},
		{"sources/bar.md", true, "default allowed"},
		{"script/lint.py", false, "not markdown"},
		{"script/readme.md", false, "default ignored"},
		{"references/guide.md", false, "default ignored"},
		{"custom/page.md", false, "not in allowed list"},
	}

	for _, tc := range cases {
		got := d.ShouldIncludeRelPath(tc.rel)
		if got != tc.want {
			t.Fatalf("%s: ShouldIncludeRelPath(%q) = %v, want %v (%s)", t.Name(), tc.rel, got, tc.want, tc.reason)
		}
	}
}

func TestWikiDirsAllowCustomDir(t *testing.T) {
	d := DefaultWikiDirs()
	d.AddAllowed("domains", "synthesis")
	if !d.ShouldIncludeRelPath("domains/inbox/entities/foo.md") {
		t.Fatal("expected custom allowed subtree to be included")
	}
	if !d.ShouldIncludeRelPath("synthesis/open-questions.md") {
		t.Fatal("expected synthesis pages to be included")
	}
}

func TestWikiDirsRestoreDefaults(t *testing.T) {
	d := DefaultWikiDirs()
	d.AddAllowed("custom")
	d.AddIgnored("drafts")
	d.RestoreDefaults()
	if len(d.Allowed) != len(LLMWikiDefaults.Allowed) {
		t.Fatalf("allowed len = %d, want %d", len(d.Allowed), len(LLMWikiDefaults.Allowed))
	}
	if len(d.Ignored) != len(LLMWikiDefaults.Ignored) {
		t.Fatalf("ignored len = %d, want %d", len(d.Ignored), len(LLMWikiDefaults.Ignored))
	}
}

func TestWikiDirsValidateOverlap(t *testing.T) {
	d := WikiDirs{
		Allowed: []string{"script"},
		Ignored: []string{"script"},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected overlap validation error")
	}
}

func TestShouldWalkDirAncestor(t *testing.T) {
	d := WikiDirs{Allowed: []string{"domains/inbox/entities"}}
	if !d.ShouldWalkDir("domains") {
		t.Fatal("expected ancestor dir domains to be walked")
	}
	if !d.ShouldWalkDir("domains/inbox") {
		t.Fatal("expected ancestor dir domains/inbox to be walked")
	}
	if d.ShouldWalkDir("concepts") {
		t.Fatal("expected unrelated dir concepts to be skipped")
	}
}
