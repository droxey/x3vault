package sync

import "testing"

func TestNeedsUploadUsesManifest(t *testing.T) {
	opts := Options{HashManifest: true}
	manifest := &HashManifest{Files: map[string]string{
		"wiki/index.md": "abc",
	}}
	if needsUpload(nil, "/ereader", "wiki/index.md", "abc", "", map[string]int64{"wiki/index.md": 10}, manifest, opts) {
		t.Fatal("expected skip when manifest hash matches")
	}
	if !needsUpload(nil, "/ereader", "wiki/index.md", "def", "", map[string]int64{"wiki/index.md": 10}, manifest, opts) {
		t.Fatal("expected upload when manifest hash differs")
	}
}

func TestParseHashManifest(t *testing.T) {
	data := []byte(`{"schema":1,"files":{"wiki/a.md":"deadbeef"}}`)
	m, err := ParseHashManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files["wiki/a.md"] != "deadbeef" {
		t.Fatalf("got %q", m.Files["wiki/a.md"])
	}
}
