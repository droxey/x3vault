#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SMOKE_TMP="$(mktemp -d)"
trap 'rm -rf "$SMOKE_TMP"' EXIT

cd "$ROOT"
# shellcheck source=scripts/version-ldflags.sh
source "$ROOT/scripts/version-ldflags.sh"
GOBIN="$SMOKE_TMP/bin" go install -ldflags "$LDFLAGS" ./cmd/x3vault
export PATH="$SMOKE_TMP/bin:$PATH"

VAULT="$SMOKE_TMP/vault"
mkdir -p "$VAULT/wiki"
printf '# Index\n\n![[attachment.txt]]\n' > "$VAULT/wiki/index.md"
printf 'Smoke attachment\n' > "$VAULT/wiki/attachment.txt"

x3vault init --vault "$VAULT" >/dev/null
# Keep doctor independent of DNS, Wi-Fi, and any real device.
sed 's|http://crosspoint.local|http://127.0.0.1:1|; s/timeout_seconds: 60/timeout_seconds: 1/' \
  "$VAULT/.xte/config.yaml" > "$SMOKE_TMP/config.yaml"
mv "$SMOKE_TMP/config.yaml" "$VAULT/.xte/config.yaml"
x3vault build --vault "$VAULT" --json > "$SMOKE_TMP/build.json"
x3vault doctor --vault "$VAULT" --json > "$SMOKE_TMP/doctor.json"
x3vault version > "$SMOKE_TMP/version.txt"

BUILD_CURRENT="$(dirname "$VAULT")/ereader/build/current"
test -f "$BUILD_CURRENT/wiki/index.md"
test -f "$BUILD_CURRENT/build.manifest"

# Parse the results using Go, so the smoke test needs no separate JSON tool.
cat > "$SMOKE_TMP/check.go" <<'GO'
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type result struct {
	Schema int
	Command string
	OK bool
	Generation string
	Summary struct { Notes, Assets, Errors int }
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	tmp, current, version, commit := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	for _, command := range []string{"build", "doctor"} {
		data, err := os.ReadFile(filepath.Join(tmp, command+".json"))
		check(err)
		var r result
		check(json.Unmarshal(data, &r))
		if r.Schema != 1 || r.Command != command || !r.OK || r.Summary.Notes != 1 || r.Summary.Errors != 0 {
			check(fmt.Errorf("unexpected %s result: %s", command, data))
		}
		if command == "build" && (r.Generation == "" || r.Summary.Assets != 1) {
			check(fmt.Errorf("build lacks generation or attachment: %s", data))
		}
	}
	want := []byte("Smoke attachment\n")
	sum := sha256.Sum256(want)
	assetPath := filepath.Join(current, "assets", hex.EncodeToString(sum[:]), "attachment.txt")
	got, err := os.ReadFile(assetPath)
	check(err)
	if string(got) != string(want) {
		check(fmt.Errorf("copied attachment differs"))
	}
	output, err := os.ReadFile(filepath.Join(tmp, "version.txt"))
	check(err)
	if strings.TrimSpace(string(output)) != fmt.Sprintf("x3vault %s (%s)", version, commit) {
		check(fmt.Errorf("version metadata mismatch: %s", output))
	}
}
GO
go run "$SMOKE_TMP/check.go" "$SMOKE_TMP" "$BUILD_CURRENT" "$VERSION" "$COMMIT"

echo "smoke test ok: JSON, version metadata, note, and attachment verified"
