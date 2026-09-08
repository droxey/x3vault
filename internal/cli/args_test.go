package cli

import "testing"

func TestVaultValueCannotBecomeAFlag(t *testing.T) {
	for _, flag := range []string{"--json", "--dry-run", "--help"} {
		if _, _, err := validateArgs([]string{"x3vault", "sync", "--vault=" + flag}); err == nil {
			t.Errorf("accepted a flag as vault value: %s", flag)
		}
	}
}
