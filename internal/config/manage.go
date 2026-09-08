package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UpdateConfig loads config, applies fn, validates, and saves.
// vault_root from the loaded config is preserved after fn runs.
func UpdateConfig(configPath string, fn func(*Config) error) (*Config, error) {
	cfg, err := LoadFromPath(configPath)
	if err != nil {
		return nil, err
	}
	vaultRoot := cfg.VaultRoot
	if err := fn(cfg); err != nil {
		return nil, err
	}
	cfg.VaultRoot = vaultRoot
	if err := Save(configPath, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// UpdateWikiDirs loads config, applies fn, validates, and saves.
func UpdateWikiDirs(configPath string, fn func(*WikiDirs) error) (*Config, error) {
	cfg, err := LoadFromPath(configPath)
	if err != nil {
		return nil, err
	}
	if err := fn(&cfg.Wiki); err != nil {
		return nil, err
	}
	if err := Save(configPath, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// MergeVaultFlag applies an explicit --vault path when config file is missing.
func MergeVaultFlag(cfg *Config, vaultFlag, configPath string) error {
	if vaultFlag == "" {
		return cfg.Resolve(configPath)
	}
	cfg.VaultRoot = vaultFlag
	return cfg.Resolve(configPath)
}

// EnsureConfigExists returns the resolved config path and config, writing defaults
// when the file is absent.
func EnsureConfigExists(vaultPath string) (string, *Config, error) {
	if vaultPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		vaultPath = cwd
	}
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", nil, err
	}
	cfgPath := ConfigPath(abs)
	if _, err := os.Stat(cfgPath); err != nil {
		if !os.IsNotExist(err) {
			return "", nil, err
		}
		cfg := Default()
		cfg.VaultRoot = abs
		if err := Save(cfgPath, cfg); err != nil {
			return "", nil, err
		}
	}
	cfg, err := LoadFromPath(cfgPath)
	if err != nil {
		return "", nil, err
	}
	return cfgPath, cfg, nil
}

// FormatWikiDirsYAML returns a human-readable summary of directory rules.
func FormatWikiDirsYAML(w WikiDirs) string {
	data, err := yaml.Marshal(map[string]any{
		"wiki": map[string]any{
			"allowed_dirs": w.Allowed,
			"ignored_dirs": w.Ignored,
		},
	})
	if err != nil {
		return ""
	}
	return string(data)
}
