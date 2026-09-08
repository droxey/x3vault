#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/x3vault"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cd "$ROOT"
go build -o "$BIN" ./cmd/x3vault

VAULT="$TMP/vault"
mkdir -p "$VAULT/wiki"
echo '# Index' > "$VAULT/wiki/index.md"

"$BIN" init --vault "$VAULT" >/dev/null
"$BIN" build --vault "$VAULT" >/dev/null
"$BIN" doctor --vault "$VAULT" >/dev/null

BUILD_CURRENT="$(dirname "$VAULT")/ereader/build/current"
test -f "$BUILD_CURRENT/wiki/index.md"
test -f "$BUILD_CURRENT/build.manifest"

echo "smoke test ok: build output at $BUILD_CURRENT"
