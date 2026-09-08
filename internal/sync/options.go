package sync

import "github.com/droxey/x3vault/internal/config"

type Options struct {
	DeviceRoot     string
	OwnershipTool  string
	FailFast       bool
	HashManifest   bool
	CleanEmptyDirs bool
}

func OptionsFromConfig(cfg *config.Config) Options {
	return Options{
		DeviceRoot:     cfg.DeviceRoot(),
		OwnershipTool:  cfg.Device.OwnershipTool,
		FailFast:       cfg.Sync.FailFast,
		HashManifest:   cfg.Sync.HashManifest,
		CleanEmptyDirs: cfg.Sync.CleanEmptyDirs,
	}
}
