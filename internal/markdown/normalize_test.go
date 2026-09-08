package markdown

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/droxey/x3vault/internal/config"
)

func noteFixture(t *testing.T, files map[string]string) (string, NormalizeOpts) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "wiki")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	var refs []NoteRef
	for rel, body := range files {
		p := filepath.Join(source, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.EqualFold(filepath.Ext(rel), ".md") {
			refs = append(refs, NoteRef{RelPath: rel, AbsPath: p})
		}
	}
	return source, NormalizeOpts{VaultRoot: root, SourceRoot: source, SourceRel: "wiki", AssetsRoot: "assets", AssetOutDir: filepath.Join(t.TempDir(), "assets"), NoteIndex: BuildNoteIndex(refs, fixtureNoteReader(t, source)).Index}
}

func normalizeFixture(t *testing.T, source, rel, body string, opts NormalizeOpts) *NormalizedNote {
	t.Helper()
	p := filepath.Join(source, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := Normalize(p, rel, opts)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNormalizePreservesMarkdownContexts(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"target.md": "# Target\n"})
	cases := []string{
		"```python\n#comment\nx = a == b == c\nprint('[[target]]')\n<!-- literal -->\n\n\n```",
		"`[[target]]` and `x == y == z`",
		"    #comment\n    x = a == b == c\n    [[target]]",
		"[Jump](#MyHeading)",
		"See issue #123 (and #456), color #ff00aa, equation x ^2.",
		"Escaped \\[[target]] and \\==literal==.",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			n := normalizeFixture(t, source, "note.md", input, opts)
			if !strings.Contains(n.Body, input) {
				t.Fatalf("literal content changed: got %q, want %q", n.Body, input)
			}
		})
	}
}

func TestNormalizeObsidianTextOnly(t *testing.T) {
	source, opts := noteFixture(t, nil)
	n := normalizeFixture(t, source, "note.md", "Hello ==highlight== text ^block-id\n<!-- secret -->\n%% private %%\nLast line\n", opts)
	if !strings.Contains(n.Body, "Hello **highlight** text") || strings.Contains(n.Body, "secret") || strings.Contains(n.Body, "private") || strings.Contains(n.Body, "^block-id") {
		t.Fatalf("body = %q", n.Body)
	}
}

func TestNormalizeNoteTargets(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"entities/target.md": "# Target", "v1.2.md": "# Version", "wiki/inner.md": "# Inner", "UPPER.MD": "# Upper"})
	cases := []struct{ input, want string }{
		{"[[v1.2]]", "[v1.2](../v1.2.md)"},
		{"![[v1.2]]", "[v1.2](../v1.2.md)"},
		{"[[#My Heading]]", "[My Heading](#my-heading)"},
		{"![[target#My Heading]]", "[My Heading](target.md#my-heading)"},
		{"![[target.md#My Heading|Title]]", "[Title](target.md#my-heading)"},
		{"[[./target]]", "[target](target.md)"},
		{"[[../v1.2]]", "[v1.2](../v1.2.md)"},
		{"[[wiki/inner]]", "[inner](../wiki/inner.md)"},
		{"[[UPPER]]", "[UPPER](../UPPER.MD)"},
		{"[[target#日本語]]", "[日本語](target.md#日本語)"},
		{"[[target#Résumé]]", "[Résumé](target.md#résumé)"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			n := normalizeFixture(t, source, "entities/note.md", tc.input, opts)
			if !strings.Contains(n.Body, tc.want) || len(n.Unresolved) != 0 || len(n.Warnings) != 0 {
				t.Fatalf("got body=%q unresolved=%q warnings=%q; want %q", n.Body, n.Unresolved, n.Warnings, tc.want)
			}
		})
	}
}

func TestNormalizeAttachmentMarkdownSyntax(t *testing.T) {
	cases := []struct{ input, file, fragment, title string }{
		{"![pic](../assets/pic.png)", "assets/pic.png", "", ""},
		{"[PDF](My%20File.pdf)", "entities/My File.pdf", "", ""},
		{"[PDF](<My File.pdf>)", "entities/My File.pdf", "", ""},
		{"![photo](photo.png \"Photo title\")", "entities/photo.png", "", "Photo title"},
		{"[report](report(v1).pdf)", "entities/report(v1).pdf", "", ""},
		{"[report](report\\(v1\\).pdf)", "entities/report(v1).pdf", "", ""},
		{"[doc](doc.pdf#page=2)", "entities/doc.pdf", "#page=2", ""},
		{"[[doc.pdf#page=2]]", "entities/doc.pdf", "#page=2", ""},
		{"![[doc.pdf#page=2]]", "entities/doc.pdf", "#page=2", ""},
		{"[doc][paper]\n\n[paper]: doc.pdf \"Paper title\"", "entities/doc.pdf", "", "Paper title"},
		{"![pic][image]\n\n[image]: ../assets/pic.png", "assets/pic.png", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			source, opts := noteFixture(t, map[string]string{tc.file: "attachment bytes"})
			n := normalizeFixture(t, source, "entities/note.md", tc.input, opts)
			if len(n.Assets) != 1 || len(n.Warnings) != 0 {
				t.Fatalf("assets=%v warnings=%v body=%q", n.Assets, n.Warnings, n.Body)
			}
			asset := n.Assets[0]
			data, err := os.ReadFile(filepath.Join(filepath.Dir(opts.AssetOutDir), filepath.FromSlash(asset.DeviceRel)))
			if err != nil || string(data) != "attachment bytes" {
				t.Fatalf("copied bytes=%q err=%v", data, err)
			}
			if !strings.Contains(n.Body, "../../assets/") || !strings.Contains(n.Body, tc.fragment) || !strings.Contains(n.Body, tc.title) {
				t.Fatalf("body=%q", n.Body)
			}
		})
	}
}

func TestNormalizeYAMLMetadata(t *testing.T) {
	cases := []struct {
		input, title string
		tags         []string
	}{
		{"---\r\ntitle: Display Title\r\ntags: [one, two]\r\n---\r\nBody\r\n", "Display Title", []string{"one", "two"}},
		{"---\ntitle: \"A # B\"\ntags:\n  - one\n  - two\n---\nBody", "A # B", []string{"one", "two"}},
		{"---\ntitle: O'Reilly\ntags: [\"a,b\", c]\n---\nBody", "O'Reilly", []string{"a,b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			source, opts := noteFixture(t, nil)
			n := normalizeFixture(t, source, "note.md", tc.input, opts)
			if n.Title != tc.title || fmt.Sprint(n.Tags) != fmt.Sprint(tc.tags) || strings.Contains(n.Body, "title:") {
				t.Fatalf("title=%q tags=%q body=%q", n.Title, n.Tags, n.Body)
			}
		})
	}
}

func TestNormalizeRejectsInvalidYAML(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"note.md": "---\ntitle: [broken\n---\nBody"})
	if _, err := Normalize(filepath.Join(source, "note.md"), "note.md", opts); err == nil {
		t.Fatal("invalid frontmatter succeeded")
	}
}

func TestNormalizeAssetCopyFailureIsFatal(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"note.md": "![[pic.png]]", "pic.png": "png"})
	if err := os.WriteFile(opts.AssetOutDir, []byte("occupied by a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(filepath.Join(source, "note.md"), "note.md", opts); err == nil {
		t.Fatal("attachment write failure was ignored")
	}
}

func TestResolveAssetUsesFullHashAndExactBytes(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"日本語.png": "bytes"})
	n := normalizeFixture(t, source, "note.md", "![[日本語.png]]", opts)
	if len(n.Assets) != 1 {
		t.Fatalf("assets=%v", n.Assets)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("bytes")))
	if n.Assets[0].HashPrefix != wantHash {
		t.Fatalf("hash=%q want full hash=%q", n.Assets[0].HashPrefix, wantHash)
	}
	if !strings.HasSuffix(n.Assets[0].DeviceRel, "/日本語.png") {
		t.Fatalf("Unicode basename lost: %s", n.Assets[0].DeviceRel)
	}
}

func TestResolveAssetRespectsCanonicalVaultBoundary(t *testing.T) {
	source, opts := noteFixture(t, nil)
	outside := filepath.Join(t.TempDir(), "synthetic.pdf")
	if err := os.WriteFile(outside, []byte("fixture bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "linked.pdf")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveAsset("linked.pdf", ".", opts); err == nil {
		t.Fatal("accepted asset symlink outside vault")
	}
	rel, err := filepath.Rel(source, outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAsset(rel, ".", opts); err == nil {
		t.Fatal("accepted asset path outside vault")
	}
}

func TestResolveAssetRejectsCanonicalExcludedTarget(t *testing.T) {
	source, opts := noteFixture(t, nil)
	cfg := config.Default()
	opts.IsExcludedVaultPath = cfg.IsExcludedVaultPath
	excluded := filepath.Join(opts.VaultRoot, ".obsidian", "settings.json")
	if err := os.MkdirAll(filepath.Dir(excluded), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(excluded, []byte("fixture settings"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(excluded, filepath.Join(source, "settings.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveAsset("settings.json", ".", opts); err == nil {
		t.Fatal("accepted symlink into excluded directory")
	}
}

func TestResolveAssetSelectedFolderDoesNotExposeMetadata(t *testing.T) {
	source, opts := noteFixture(t, nil)
	metadata := filepath.Join(opts.VaultRoot, ".x3vault")
	if err := os.MkdirAll(metadata, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "private.json"), []byte("synthetic metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.AttachmentFolder = metadata
	if _, err := resolveAsset("private.json", filepath.Base(source), opts); err == nil {
		t.Fatal("selected folder exposed metadata")
	}
}

func TestResolveAssetSourceAssetsFallback(t *testing.T) {
	_, opts := noteFixture(t, map[string]string{"media/picture.png": "image bytes"})
	opts.AssetsRoot = "media"
	if _, err := resolveAsset("picture.png", "entities", opts); err != nil {
		t.Fatalf("configured source assets fallback: %v", err)
	}
}

func TestResolveAssetIgnoredSymlinkPath(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"picture.png": "image bytes"})
	if err := os.MkdirAll(filepath.Join(source, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "picture.png"), filepath.Join(source, "script", "copy.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dirs := config.DefaultWikiDirs()
	opts.ShouldIncludeSourceRel = dirs.ShouldIncludeRelPath
	if _, err := resolveAsset("script/copy.png", ".", opts); err == nil {
		t.Fatal("ignored source path was copied through a symlink")
	}
}

func TestNormalizeCommentsDoNotCopyHiddenAttachments(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"pic.png": "image bytes"})
	n := normalizeFixture(t, source, "note.md", "Before %% hidden\n\n![pic](pic.png)\n\nend %% After", opts)
	if len(n.Assets) != 0 || strings.Contains(n.Body, "pic.png") || !strings.Contains(n.Body, "After") {
		t.Fatalf("assets=%v body=%q", n.Assets, n.Body)
	}
}

func TestNormalizeHighlightPreservesNestedCode(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"target.md": "# Target"})
	n := normalizeFixture(t, source, "note.md", "==`a == b`== and ==[[target]]==", opts)
	if !strings.Contains(n.Body, "**`a == b`** and **[target](target.md)**") {
		t.Fatalf("body=%q", n.Body)
	}
}

func TestNormalizeEncodedAttachmentName(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"100%#done.pdf": "pdf bytes"})
	n := normalizeFixture(t, source, "note.md", "[doc](100%25%23done.pdf#page=2)", opts)
	if len(n.Assets) != 1 || !strings.Contains(n.Body, "/100%25%23done.pdf#page=2)") {
		t.Fatalf("assets=%v body=%q", n.Assets, n.Body)
	}
}

func TestNormalizeDoesNotMergeCollidingAssetPrefixes(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"a/picture.png": "fixture attachment 54", "b/picture.png": "fixture attachment 313"})
	n := normalizeFixture(t, source, "note.md", "![[a/picture.png]] ![[b/picture.png]]", opts)
	if len(n.Assets) != 2 || n.Assets[0].DeviceRel == n.Assets[1].DeviceRel {
		t.Fatalf("colliding attachments merged: %v", n.Assets)
	}
	for _, asset := range n.Assets {
		got, err := os.ReadFile(filepath.Join(filepath.Dir(opts.AssetOutDir), filepath.FromSlash(asset.DeviceRel)))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(asset.SourceAbs)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("attachment changed: got %q want %q", got, want)
		}
	}
}

func TestNormalizeCanceled(t *testing.T) {
	source, opts := noteFixture(t, map[string]string{"note.md": "body"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts.Context = ctx
	if _, err := Normalize(filepath.Join(source, "note.md"), "note.md", opts); err != context.Canceled {
		t.Fatalf("error=%v want context cancellation", err)
	}
}

func TestNormalizeLinkedImage(t *testing.T) {
	for _, outer := range []string{"note.md", "https://example.test/", "doc.pdf"} {
		t.Run(outer, func(t *testing.T) {
			source, opts := noteFixture(t, map[string]string{"pic.png": "png bytes", "doc.pdf": "pdf bytes"})
			n := normalizeFixture(t, source, "note.md", "[![**picture**](pic.png)]("+outer+")", opts)
			if !strings.Contains(n.Body, "[![**picture**](../assets/") {
				t.Fatalf("linked image was not rewritten: %q", n.Body)
			}
			wantAssets := 1
			if outer == "doc.pdf" {
				wantAssets = 2
			}
			if len(n.Assets) != wantAssets || len(n.Warnings) != 0 {
				t.Fatalf("assets=%v warnings=%v", n.Assets, n.Warnings)
			}
		})
	}
}
