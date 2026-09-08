package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownKeysAndMultipleDocuments(t *testing.T) {
	for name, input := range map[string]string{
		"unknown top level":        "scheam: 1\n",
		"unknown nested":           "sync:\n  hash_manfiest: false\n",
		"second document":          "schema: 1\n---\nschema: 999\n",
		"empty second document":    "schema: 1\n---\n",
		"invalid ignored absolute": "wiki:\n  ignored_dirs: [/tmp/victim]\n",
		"invalid ignored parent":   "wiki:\n  ignored_dirs: [safe/../../victim]\n",
		"invalid standard parent":  "wiki:\n  standard_dirs: [safe/../victim]\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

func TestConfigRejectsUnsafePathsAndDeviceValues(t *testing.T) {
	cases := map[string]func(*Config){
		"absolute source":       func(c *Config) { c.SourceRoot = "/wiki" },
		"source drive":          func(c *Config) { c.SourceRoot = "C:/wiki" },
		"source backslash":      func(c *Config) { c.SourceRoot = `wiki\nested` },
		"source dot segment":    func(c *Config) { c.SourceRoot = "wiki/./nested" },
		"source metadata":       func(c *Config) { c.SourceRoot = "_meta/wiki" },
		"source obsidian":       func(c *Config) { c.SourceRoot = ".obsidian" },
		"assets metadata":       func(c *Config) { c.Build.AssetsRoot = "_meta/assets" },
		"source assets overlap": func(c *Config) { c.Build.AssetsRoot = "wiki/assets" },
		"assets source overlap": func(c *Config) { c.SourceRoot = "assets/wiki" },
		"device dot root":       func(c *Config) { c.Device.Root = "/." },
		"device parent root":    func(c *Config) { c.Device.Root = "/a/.." },
		"device backslash":      func(c *Config) { c.Device.Root = `/ereader\escape` },
		"device double slash":   func(c *Config) { c.Device.Root = "/a//b" },
		"url no scheme":         func(c *Config) { c.Device.BaseURL = "crosspoint.local" },
		"url no host":           func(c *Config) { c.Device.BaseURL = "http:///path" },
		"url ftp":               func(c *Config) { c.Device.BaseURL = "ftp://crosspoint.local" },
		"url fragment":          func(c *Config) { c.Device.BaseURL = "http://crosspoint.local/#foo" },
		"negative timeout":      func(c *Config) { c.Device.TimeoutSeconds = -1 },
		"timeout overflow":      func(c *Config) { c.Device.TimeoutSeconds = 1 << 40 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			mutate(c)
			c.Normalize()
			if err := c.Validate(); err == nil {
				t.Fatal("accepted unsafe config")
			}
		})
	}
	c := Default()
	c.SourceRoot = "wiki..old"
	if err := c.Validate(); err != nil {
		t.Fatalf("ordinary dots in name: %v", err)
	}
}

func TestResolveRejectsCanonicalBuildOverlap(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	if err := os.Mkdir(vault, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(vault, alias); err != nil {
		t.Skip(err)
	}
	for _, root := range []string{alias, filepath.Join(alias, "missing", "build"), parent} {
		c := Default()
		c.VaultRoot = vault
		c.BuildRoot = root
		if err := c.Resolve(ConfigPath(vault)); err == nil {
			t.Errorf("accepted overlapping build root %s", root)
		}
	}
}

func TestSaveRejectsSymlinkTargetsAndDirectories(t *testing.T) {
	for _, parentLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "parent"}[parentLink], func(t *testing.T) {
			vault := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "config.yaml")
			if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			p := ConfigPath(vault)
			if parentLink {
				if err := os.Symlink(outside, filepath.Dir(p)); err != nil {
					t.Skip(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, p); err != nil {
					t.Skip(err)
				}
			}
			if err := Save(p, Default()); err == nil {
				t.Fatal("saved through symlink")
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "keep" {
				t.Fatal("modified external config")
			}
		})
	}
}

func TestUpdatePreservesRelativeSerializedPaths(t *testing.T) {
	vault := t.TempDir()
	p := ConfigPath(vault)
	c := Default()
	c.BuildRoot = "../custom/build"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConfig(p, func(c *Config) error { c.Device.Root = "/custom"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateWikiDirs(p, func(w *WikiDirs) error { w.AddIgnored("drafts"); return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.VaultRoot != "." || got.BuildRoot != "../custom/build" {
		t.Fatalf("serialized paths changed: vault=%q build=%q", got.VaultRoot, got.BuildRoot)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), vault) {
		t.Fatal("absolute vault path persisted")
	}
}

func TestExplicitAttachmentFolderTakesPrecedence(t *testing.T) {
	vault := t.TempDir()
	obs := filepath.Join(vault, ".obsidian")
	if err := os.Mkdir(obs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obs, "app.json"), []byte(`{"attachmentFolderPath":"raw/assets"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := Default()
	c.VaultRoot = vault
	c.Build.AttachmentFolder = "chosen"
	if got, want := c.ResolveAttachmentFolderAbs(), filepath.Join(vault, "chosen"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSaveAtomicallyReplacesExistingFile(t *testing.T) {
	p := ConfigPath(t.TempDir())
	if err := Save(p, Default()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "original.yaml")
	if err := os.Link(p, snapshot); err != nil {
		t.Skip(err)
	}
	cfg := Default()
	cfg.Device.Root = "/updated"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(old) != string(before) {
		t.Fatal("save overwrote the original inode")
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("changed config permissions to %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files remain: %v", entries)
	}
}

func TestResolveConfigPathDoesNotSkipDanglingPrimary(t *testing.T) {
	vault := t.TempDir()
	primary := ConfigPath(vault)
	if err := os.MkdirAll(filepath.Dir(primary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vault, "missing"), primary); err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(vault, LegacyConfigFileName), []byte("schema: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveConfigPath(vault); got != primary {
		t.Fatalf("skipped dangling primary: %s", got)
	}
	if _, err := Load(primary); err == nil || os.IsNotExist(err) {
		t.Fatalf("symlink config must fail closed, got %v", err)
	}
}

func TestAttachmentFoldersRelativeToNote(t *testing.T) {
	vault := t.TempDir()
	obs := filepath.Join(vault, ".obsidian")
	if err := os.Mkdir(obs, 0755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(vault, "wiki", "topic", "note.md")
	cfg := Default()
	cfg.VaultRoot = vault
	for _, folder := range []string{"./attachments", ".", "./"} {
		if err := os.WriteFile(filepath.Join(obs, "app.json"), []byte(`{"attachmentFolderPath":"`+folder+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
		want := filepath.Dir(note)
		if folder == "./attachments" {
			want = filepath.Join(want, "attachments")
		}
		if got := cfg.ResolveAttachmentFolderForNoteAbs(note); got != want {
			t.Errorf("%s: got %q want %q", folder, got, want)
		}
		if got := cfg.ResolveAttachmentFolderAbs(); got != "" {
			t.Errorf("%s: returned vault-relative folder %q", folder, got)
		}
	}
}

func TestCanonicalDeviceRoot(t *testing.T) {
	if got, err := CanonicalDeviceRoot(" /reader/notes/ "); err != nil || got != "/reader/notes" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, root := range []string{"/", "/.", "/a/..", "/a/../b", "/a//b", `/a\b`, "/a\x00b", "relative"} {
		if _, err := CanonicalDeviceRoot(root); err == nil {
			t.Errorf("accepted %q", root)
		}
	}
}

func TestResolveConfigPathDoesNotSkipSymlinkedConfigDirectory(t *testing.T) {
	vault := t.TempDir()
	primary := ConfigPath(vault)
	if err := os.Symlink(filepath.Join(vault, "missing"), filepath.Dir(primary)); err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(vault, LegacyConfigFileName), []byte("schema: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveConfigPath(vault); got != primary {
		t.Fatalf("skipped unsafe primary directory: %s", got)
	}
}

func TestLoadRejectsNullAndEmptyAssets(t *testing.T) {
	for _, data := range []string{"null\n", "build:\n  assets_root: ''\n"} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}

func TestLegacyConfigWorksThroughVaultAlias(t *testing.T) {
	vault := t.TempDir()
	p := filepath.Join(vault, LegacyConfigFileName)
	if err := Save(p, Default()); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "vault")
	if err := os.Symlink(vault, alias); err != nil {
		t.Skip(err)
	}
	aliasPath := ResolveConfigPath(alias)
	if _, err := LoadFromPath(aliasPath); err != nil {
		t.Fatalf("load through vault alias: %v", err)
	}
	if _, err := UpdateWikiDirs(aliasPath, func(w *WikiDirs) error { w.AddIgnored("drafts"); return nil }); err != nil {
		t.Fatalf("update through vault alias: %v", err)
	}
}

func TestConfigRejectsBuildManifestNamespace(t *testing.T) {
	for _, field := range []string{"source_root", "build.assets_root"} {
		for _, root := range []string{"build.manifest", "build.manifest/nested"} {
			t.Run(field+"="+root, func(t *testing.T) {
				cfg := Default()
				if field == "source_root" {
					cfg.SourceRoot = root
				} else {
					cfg.Build.AssetsRoot = root
				}
				if err := cfg.Validate(); err == nil {
					t.Fatal("accepted namespace reserved for generated build summary")
				}
			})
		}
	}
	cfg := Default()
	cfg.SourceRoot = "build.manifest-notes"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unrelated namespace rejected: %v", err)
	}
}
