package config

import (
	"testing"
	"time"
)

func TestDefaultConfigValidates(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIsExcludedVaultPath(t *testing.T) {
	cfg := Default()
	for _, p := range []string{"raw/foo.md", ".obsidian/app.json", ".git/config", ".xte/build/x", ".x3vault/build/x"} {
		if !cfg.IsExcludedVaultPath(p) {
			t.Fatalf("expected excluded: %s", p)
		}
	}
	if cfg.IsExcludedVaultPath("wiki/index.md") {
		t.Fatal("wiki paths should not be excluded")
	}
}

func TestDeviceTimeoutDefault(t *testing.T) {
	cfg := Default()
	if cfg.DeviceTimeout() != 60*time.Second {
		t.Fatalf("timeout = %v", cfg.DeviceTimeout())
	}
	cfg.Device.TimeoutSeconds = 0
	cfg.Normalize()
	if cfg.DeviceTimeout() != 60*time.Second {
		t.Fatalf("normalized timeout = %v", cfg.DeviceTimeout())
	}
}

func TestRestoreDefaultsPreservingVault(t *testing.T) {
	cfg := Default()
	cfg.VaultRoot = "/tmp/my-vault"
	cfg.Device.Root = "/custom"
	cfg.RestoreDefaultsPreservingVault()
	if cfg.VaultRoot != "/tmp/my-vault" {
		t.Fatalf("vault_root = %q", cfg.VaultRoot)
	}
	if cfg.Device.Root != DefaultDevice().Root {
		t.Fatalf("device.root = %q", cfg.Device.Root)
	}
}
