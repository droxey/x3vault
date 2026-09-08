package sync

import (
	"context"
	"strings"
	"testing"
)

func TestNeedsUploadUsesManifest(t *testing.T) {
	opts := Options{HashManifest: true}
	manifest := &HashManifest{Files: map[string]string{"wiki/index.md": "abc"}}
	upload, err := needsUpload(context.Background(), nil, "/ereader", "wiki/index.md", "abc", 10, map[string]int64{"wiki/index.md": 10}, manifest, opts)
	if err != nil || upload {
		t.Fatalf("matching manifest: upload=%v err=%v", upload, err)
	}
	upload, err = needsUpload(context.Background(), newMemoryDevice(), "/ereader", "wiki/index.md", "def", 10, map[string]int64{"wiki/index.md": 10}, manifest, opts)
	if err != nil || !upload {
		t.Fatalf("different manifest: upload=%v err=%v", upload, err)
	}
}

func TestParseHashManifest(t *testing.T) {
	hash := strings.Repeat("a", 64)
	data := []byte(`{"schema":1,"files":{"wiki/a.md":"` + hash + `"}}`)
	m, err := ParseHashManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files["wiki/a.md"] != hash {
		t.Fatalf("got %q", m.Files["wiki/a.md"])
	}
	for _, data := range []string{`null`, `{}`, `{"schema":2}`, `{"schema":1,"files":{"../escape":"` + hash + `"}}`, `{"schema":1,"files":{"_meta/receipt":"` + hash + `"}}`, `{"schema":1,"files":{"note.md":"short"}}`} {
		if _, err := ParseHashManifest([]byte(data)); err == nil {
			t.Fatalf("accepted invalid manifest: %s", data)
		}
	}
}
