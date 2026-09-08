package config

// UpdateConfig edits serialized values, preserving vault_root and leaving
// relative paths relative. Resolve validates a copy before the atomic save.
func UpdateConfig(configPath string, fn func(*Config) error) (*Config, error) {
	cfg, err := Load(configPath)
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

// UpdateWikiDirs shares the config update path so validation and persistence
// have the same behavior for both commands.
func UpdateWikiDirs(configPath string, fn func(*WikiDirs) error) (*Config, error) {
	return UpdateConfig(configPath, func(cfg *Config) error { return fn(&cfg.Wiki) })
}
