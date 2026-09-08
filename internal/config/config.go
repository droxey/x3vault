package config

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/droxey/x3vault/internal/obsidian"
	"github.com/droxey/x3vault/internal/pathutil"
	"go.yaml.in/yaml/v3"
)

const SchemaVersion = 1

// Tool paths and device defaults.
const (
	ConfigDirName               = ".xte"
	ConfigFileBasename          = "config.yaml"
	LegacyEreaderConfigFileName = ".ereader.yaml"
	LegacyXTEConfigFileName     = ".xte.yaml"
	LegacyConfigFileName        = ".x3vault.yaml"
	EreaderDirName              = "ereader"
	DefaultBuildRootRel         = "../ereader/build"
	DefaultAssetsRoot           = "assets"
	DefaultDeviceRoot           = "/ereader"
	DefaultOwnershipTool        = "ereader"
)

type BuildConfig struct {
	AssetsRoot         string `yaml:"assets_root"`
	AttachmentFolder   string `yaml:"attachment_folder"`
	ReadObsidianConfig bool   `yaml:"read_obsidian_config"`
}

type SyncConfig struct {
	FailFast          bool     `yaml:"fail_fast"`
	HashManifest      bool     `yaml:"hash_manifest"`
	CleanEmptyDirs    bool     `yaml:"clean_empty_dirs"`
	ExcludeVaultPaths []string `yaml:"exclude_vault_paths"`
}

type DeviceConfig struct {
	BaseURL        string `yaml:"base_url"`
	Root           string `yaml:"root"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	OwnershipTool  string `yaml:"ownership_tool"`
}

type Config struct {
	Schema     int          `yaml:"schema"`
	VaultRoot  string       `yaml:"vault_root"`
	SourceRoot string       `yaml:"source_root"`
	BuildRoot  string       `yaml:"build_root"`
	Wiki       WikiDirs     `yaml:"wiki"`
	Build      BuildConfig  `yaml:"build"`
	Sync       SyncConfig   `yaml:"sync"`
	Device     DeviceConfig `yaml:"device"`
}

func DefaultBuild() BuildConfig {
	return BuildConfig{
		AssetsRoot:         DefaultAssetsRoot,
		AttachmentFolder:   "",
		ReadObsidianConfig: true,
	}
}

func DefaultSync() SyncConfig {
	return SyncConfig{
		FailFast:          true,
		HashManifest:      true,
		CleanEmptyDirs:    true,
		ExcludeVaultPaths: []string{"raw", ".obsidian", ".git", ".x3vault", ConfigDirName},
	}
}

func DefaultDevice() DeviceConfig {
	return DeviceConfig{
		BaseURL:        "http://crosspoint.local",
		Root:           DefaultDeviceRoot,
		TimeoutSeconds: 60,
		OwnershipTool:  DefaultOwnershipTool,
	}
}

func Default() *Config {
	return &Config{
		Schema:     SchemaVersion,
		VaultRoot:  ".",
		SourceRoot: "wiki",
		BuildRoot:  DefaultBuildRootRel,
		Wiki:       DefaultWikiDirs(),
		Build:      DefaultBuild(),
		Sync:       DefaultSync(),
		Device:     DefaultDevice(),
	}
}

func Load(path string) (*Config, error) {
	if err := validateConfigTarget(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("config must be a YAML mapping")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		return nil, fmt.Errorf("config must contain exactly one YAML document")
	}
	if cfg.Wiki.Mode == "" && len(cfg.Wiki.Ignored) == 0 && len(cfg.Wiki.StandardDirs) == 0 {
		cfg.Wiki = DefaultWikiDirs()
	} else if cfg.Wiki.Mode == "" {
		cfg.Wiki.Mode = WikiModeAllExceptIgnored
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Normalize() {
	c.Wiki.Normalize()
	if c.Wiki.Mode == "" {
		c.Wiki.Mode = WikiModeAllExceptIgnored
	}
	if len(c.Wiki.StandardDirs) == 0 {
		c.Wiki.StandardDirs = append([]string(nil), LLMWikiStandardDirs...)
	}
	if strings.TrimSpace(c.Device.OwnershipTool) == "" {
		c.Device.OwnershipTool = DefaultDevice().OwnershipTool
	}
	c.Sync.ExcludeVaultPaths = normalizeDirList(c.Sync.ExcludeVaultPaths)
}

func (c *Config) Validate() error {
	if c.Schema != SchemaVersion {
		return fmt.Errorf("unsupported config schema %d (want %d)", c.Schema, SchemaVersion)
	}
	if err := validateRelPath(c.SourceRoot, "source_root"); err != nil {
		return err
	}
	if strings.TrimSpace(c.VaultRoot) == "" {
		return fmt.Errorf("vault_root must not be empty")
	}
	if err := c.Wiki.Validate(); err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	if err := validateRelPath(c.Build.AssetsRoot, "build.assets_root"); err != nil {
		return err
	}
	for field, root := range map[string]string{"source_root": c.SourceRoot, "build.assets_root": c.Build.AssetsRoot} {
		first := strings.Split(root, "/")[0]
		switch first {
		case "_meta", "build.manifest", ".obsidian", ".git", ".xte", ".x3vault":
			return fmt.Errorf("%s uses reserved namespace %q", field, first)
		}
	}
	if pathutil.ContainedIn(c.SourceRoot, c.Build.AssetsRoot) || pathutil.ContainedIn(c.Build.AssetsRoot, c.SourceRoot) {
		return fmt.Errorf("source_root and build.assets_root must not overlap")
	}
	if c.Build.AttachmentFolder != "" {
		if err := validateRelPath(c.Build.AttachmentFolder, "build.attachment_folder"); err != nil {
			return err
		}
	}
	for _, p := range c.Sync.ExcludeVaultPaths {
		if err := validateRelPath(p, "sync.exclude_vault_paths"); err != nil {
			return err
		}
	}
	u, err := url.Parse(c.Device.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("device.base_url must be an http(s) URL with a host and no credentials, query, or fragment")
	}
	if c.Device.TimeoutSeconds <= 0 || int64(c.Device.TimeoutSeconds) > int64((1<<63-1)/time.Second) {
		return fmt.Errorf("device.timeout_seconds must be positive and fit in a time.Duration")
	}
	if err := validateDeviceRoot(c.Device.Root); err != nil {
		return err
	}
	return validateBuildRootRef(c.BuildRoot)
}

func validateBuildRootRef(buildRoot string) error {
	buildRoot = strings.TrimSpace(buildRoot)
	if buildRoot == "" {
		return fmt.Errorf("build_root must not be empty")
	}
	return nil
}

// ValidateBuildRootOutsideVault rejects either-direction canonical overlap.
func ValidateBuildRootOutsideVault(buildRoot, vaultRoot string) error {
	buildAbs, err := pathutil.Canonical(buildRoot)
	if err != nil {
		return fmt.Errorf("build_root canonical path: %w", err)
	}
	vaultAbs, err := pathutil.Canonical(vaultRoot)
	if err != nil {
		return fmt.Errorf("vault_root canonical path: %w", err)
	}
	if pathutil.ContainedIn(buildAbs, vaultAbs) || pathutil.ContainedIn(vaultAbs, buildAbs) {
		return fmt.Errorf("build_root must not overlap the Obsidian vault (%s)", vaultAbs)
	}
	return nil
}

func validateRelPath(p, field string) error {
	if err := pathutil.ValidateRelative(p); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func validateDeviceRoot(root string) error {
	_, err := CanonicalDeviceRoot(root)
	return err
}

// CanonicalDeviceRoot validates an absolute POSIX device namespace. Only an
// optional trailing slash and outer whitespace are normalized.
func CanonicalDeviceRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	root = strings.TrimSuffix(root, "/")
	if !strings.HasPrefix(root, "/") || root == "" {
		return "", fmt.Errorf("device.root must be an absolute non-root device path (e.g. %s)", DefaultDeviceRoot)
	}
	if strings.Contains(root, `\`) || strings.IndexFunc(root, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("device.root contains invalid characters")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("device.root must not contain empty, dot, or parent path segments")
		}
	}
	return root, nil
}

func (c *Config) DeviceRoot() string {
	root, _ := CanonicalDeviceRoot(c.Device.Root)
	return root
}

func (c *Config) DeviceTimeout() time.Duration {
	return time.Duration(c.Device.TimeoutSeconds) * time.Second
}

func (c *Config) IsExcludedVaultPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, ex := range c.Sync.ExcludeVaultPaths {
		ex = cleanDirEntry(ex)
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return false
}

func (c *Config) RestoreDefaults() {
	*c = *Default()
}

func (c *Config) RestoreDefaultsPreservingVault() {
	vault := c.VaultRoot
	c.RestoreDefaults()
	c.VaultRoot = vault
}

func (c *Config) Resolve(configPath string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	vaultBase := VaultRootFromConfigPath(configPath)
	if !filepath.IsAbs(c.VaultRoot) {
		c.VaultRoot = filepath.Join(vaultBase, c.VaultRoot)
	}
	abs, err := filepath.Abs(c.VaultRoot)
	if err != nil {
		return fmt.Errorf("vault_root abs: %w", err)
	}
	c.VaultRoot = abs

	if !filepath.IsAbs(c.BuildRoot) {
		c.BuildRoot = filepath.Join(c.VaultRoot, c.BuildRoot)
	}
	buildAbs, err := filepath.Abs(c.BuildRoot)
	if err != nil {
		return fmt.Errorf("build_root abs: %w", err)
	}
	c.BuildRoot = buildAbs
	return ValidateBuildRootOutsideVault(c.BuildRoot, c.VaultRoot)
}

func VaultRootFromConfigPath(configPath string) string {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		abs = configPath
	}
	dir := filepath.Dir(abs)
	if filepath.Base(dir) == ConfigDirName {
		return filepath.Dir(dir)
	}
	return dir
}

func ConfigPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, ConfigDirName, ConfigFileBasename)
}

// ResolveConfigPath returns the config file to use, preferring .xte/config.yaml with
// fallbacks to legacy vault-root config files when present.
func ResolveConfigPath(vaultRoot string) string {
	primary := ConfigPath(vaultRoot)
	if info, err := os.Lstat(filepath.Dir(primary)); err == nil {
		if !info.IsDir() {
			return primary
		}
	} else if !os.IsNotExist(err) {
		return primary
	}
	if _, err := os.Lstat(primary); !os.IsNotExist(err) {
		return primary
	}
	for _, name := range []string{LegacyEreaderConfigFileName, LegacyXTEConfigFileName, LegacyConfigFileName} {
		p := filepath.Join(vaultRoot, name)
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			return p
		}
	}
	return primary
}

func (c *Config) SourceDir() string {
	return filepath.Join(c.VaultRoot, c.SourceRoot)
}

// ResolveAttachmentFolderAbs returns the vault directory Obsidian (or config) uses
// for bare embed names like ![[paper.pdf]]. Empty when unset.
func (c *Config) ResolveAttachmentFolderAbs() string {
	return c.ResolveAttachmentFolderForNoteAbs("")
}

// ResolveAttachmentFolderForNoteAbs applies explicit vault-relative overrides
// before Obsidian settings, including Obsidian's per-note ./folder convention.
func (c *Config) ResolveAttachmentFolderForNoteAbs(noteAbs string) string {
	if c.Build.AttachmentFolder != "" {
		return filepath.Join(c.VaultRoot, filepath.FromSlash(c.Build.AttachmentFolder))
	}
	if c.Build.ReadObsidianConfig {
		rel := obsidian.AttachmentFolder(c.VaultRoot)
		if rel == "." || strings.HasPrefix(rel, "./") {
			if noteAbs == "" {
				return ""
			}
			return obsidian.ResolveAttachmentPath(filepath.Dir(noteAbs), strings.TrimPrefix(rel, "./"))
		}
		return obsidian.ResolveAttachmentPath(c.VaultRoot, rel)
	}
	return ""
}

func WriteDefault(path string) error {
	cfg := Default()
	return Save(path, cfg)
}

func LoadFromPath(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Resolve(path); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validateConfigTarget prevents config reads and writes from following a
// symlinked config file or the tool-owned .xte directory.
func validateConfigTarget(path string) error {
	candidates := []string{path}
	if filepath.Base(filepath.Dir(path)) == ConfigDirName {
		candidates = append(candidates, filepath.Dir(path))
	}
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect config path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("config path must not be a symlink: %s", candidate)
		}
		if candidate == path && !info.Mode().IsRegular() {
			return fmt.Errorf("config must be a regular file: %s", path)
		}
	}
	return nil
}

func Save(path string, cfg *Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	resolved := *cfg
	if err := resolved.Resolve(path); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := validateConfigTarget(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Lstat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := validateConfigTarget(path); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func FormatConfigYAML(cfg *Config) string {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(data)
}
