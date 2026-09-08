package sync

import "testing"

func TestAppendEmptyDirDeletes(t *testing.T) {
	plan := &Plan{}
	local := map[string]string{"wiki/index.md": "abc"}
	remoteFiles := map[string]int64{
		"wiki/old/note.md": 10,
	}
	remoteDirs := map[string]bool{
		"wiki":     true,
		"wiki/old": true,
	}
	fileDeletes := []string{"wiki/old/note.md"}

	appendEmptyDirDeletes(plan, local, remoteFiles, fileDeletes, remoteDirs, "/ereader")

	if len(plan.Deletes) != 1 {
		t.Fatalf("deletes = %d, want 1 empty dir", len(plan.Deletes))
	}
	if plan.Deletes[0].Type != "directory" || plan.Deletes[0].Path != "/ereader/wiki/old" {
		t.Fatalf("delete op = %+v", plan.Deletes[0])
	}
}

func TestAppendEmptyDirDeletesSkipsMeta(t *testing.T) {
	plan := &Plan{}
	appendEmptyDirDeletes(plan, map[string]string{}, map[string]int64{}, nil,
		map[string]bool{"_meta": true, "_meta/cache": true}, "/ereader")
	if len(plan.Deletes) != 0 {
		t.Fatalf("expected no _meta deletes, got %d", len(plan.Deletes))
	}
}
