package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

type DeviceConfig struct {
	BaseURL string `yaml:"base_url"`
	Root    string `yaml:"root"`
}

type Config struct {
	Schema     int          `yaml:"schema"`
	VaultRoot  string       `yaml:"vault_root"`
	SourceRoot string       `yaml:"source_root"`
	BuildRoot  string       `yaml:"build_root"`
	EPUB       bool         `yaml:"epub"`
	Device     DeviceConfig `yaml:"device"`
}

func Default() *Config {
	return &Config{
		Schema:     SchemaVersion,
		VaultRoot:  ".",
		SourceRoot: "wiki",
		BuildRoot:  ".x3vault/build",
		EPUB:       false,
		Device: DeviceConfig{
			BaseURL: "http://crosspoint.local",
			Root:    "/x3vault",
		},
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
	if cfg.Schema != SchemaVersion {
		return nil, fmt.Errorf("unsupported config schema %d (want %d)", cfg.Schema, SchemaVersion)
	}
	if cfg.SourceRoot != "wiki" {
		return nil, fmt.Errorf("v0 requires source_root: wiki (got %q)", cfg.SourceRoot)
	}
	return cfg, nil
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
	return nil
}

func (c *Config) SourceDir() string {
	return filepath.Join(c.VaultRoot, c.SourceRoot)
}

func WriteDefault(path string) error {
	cfg := Default()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
