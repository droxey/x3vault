package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

// Tool paths and device defaults (formerly .x3vault / /x3vault).
const (
	ConfigFileName       = ".xte.yaml"
	LegacyConfigFileName = ".x3vault.yaml"
	ToolDirName          = ".xte"
	DefaultBuildRootRel  = "../xte/build"
	DefaultDeviceRoot    = "/xte"
	DefaultOwnershipTool = "xte"
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
	EPUB       bool         `yaml:"epub"`
	Wiki       WikiDirs     `yaml:"wiki"`
	Build      BuildConfig  `yaml:"build"`
	Sync       SyncConfig   `yaml:"sync"`
	Device     DeviceConfig `yaml:"device"`
}

func DefaultBuild() BuildConfig {
	return BuildConfig{
		AssetsRoot:         "assets",
		AttachmentFolder:   "",
		ReadObsidianConfig: true,
	}
}

func DefaultSync() SyncConfig {
	return SyncConfig{
		FailFast:          true,
		HashManifest:      true,
		CleanEmptyDirs:    true,
		ExcludeVaultPaths: []string{"raw", ".obsidian", ".git", ".x3vault"},
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
		EPUB:       false,
		Wiki:       DefaultWikiDirs(),
		Build:      DefaultBuild(),
		Sync:       DefaultSync(),
		Device:     DefaultDevice(),
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
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
	if c.Build.AssetsRoot == "" {
		c.Build.AssetsRoot = DefaultBuild().AssetsRoot
	}
	if c.Device.TimeoutSeconds <= 0 {
		c.Device.TimeoutSeconds = DefaultDevice().TimeoutSeconds
	}
	if strings.TrimSpace(c.Device.OwnershipTool) == "" {
		c.Device.OwnershipTool = DefaultDevice().OwnershipTool
	}
	c.Sync.ExcludeVaultPaths = normalizeDirList(c.Sync.ExcludeVaultPaths)
	if len(c.Sync.ExcludeVaultPaths) == 0 && c.Sync.FailFast == false && !c.Sync.HashManifest && !c.Sync.CleanEmptyDirs {
		// zero value sync block in yaml — defaults already applied via Default() merge
	}
}

func (c *Config) Validate() error {
	if c.Schema != SchemaVersion {
		return fmt.Errorf("unsupported config schema %d (want %d)", c.Schema, SchemaVersion)
	}
	if strings.TrimSpace(c.SourceRoot) == "" {
		return fmt.Errorf("source_root must not be empty")
	}
	if strings.Contains(c.SourceRoot, "..") {
		return fmt.Errorf("source_root must not contain ..")
	}
	if err := c.Wiki.Validate(); err != nil {
		return fmt.Errorf("wiki: %w", err)
	}
	if err := validateRelPath(c.Build.AssetsRoot, "build.assets_root"); err != nil {
		return err
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
	if strings.TrimSpace(c.Device.BaseURL) == "" {
		return fmt.Errorf("device.base_url must not be empty")
	}
	return validateDeviceRoot(c.Device.Root)
}

func validateRelPath(p, field string) error {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" || p == "." {
		return fmt.Errorf("%s must be a non-empty relative path", field)
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%s must be relative", field)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("%s must not contain ..", field)
	}
	return nil
}

func validateDeviceRoot(root string) error {
	root = cleanDeviceRoot(root)
	if root == "" || root == "/" {
		return fmt.Errorf("device.root must be an absolute device path (e.g. %s)", DefaultDeviceRoot)
	}
	if !strings.HasPrefix(root, "/") {
		return fmt.Errorf("device.root must start with / (got %q)", root)
	}
	return nil
}

func cleanDeviceRoot(root string) string {
	root = strings.TrimSpace(root)
	root = filepath.ToSlash(root)
	return strings.TrimSuffix(root, "/")
}

func (c *Config) DeviceRoot() string {
	return cleanDeviceRoot(c.Device.Root)
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
	base := filepath.Dir(configPath)
	if !filepath.IsAbs(c.VaultRoot) {
		c.VaultRoot = filepath.Join(base, c.VaultRoot)
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
	return nil
}

func ConfigPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, ConfigFileName)
}

// ResolveConfigPath returns the config file to use, preferring .xte.yaml with
// a fallback to legacy .x3vault.yaml when present.
func ResolveConfigPath(vaultRoot string) string {
	primary := ConfigPath(vaultRoot)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	legacy := filepath.Join(vaultRoot, LegacyConfigFileName)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return primary
}

func (c *Config) SourceDir() string {
	return filepath.Join(c.VaultRoot, c.SourceRoot)
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

func Save(path string, cfg *Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func FormatConfigYAML(cfg *Config) string {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(data)
}
