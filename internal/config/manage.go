package config

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
