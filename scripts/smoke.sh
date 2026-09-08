#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cd "$ROOT"
GOBIN="$ROOT/bin" go install ./cmd/x3vault
export PATH="$ROOT/bin:$PATH"

VAULT="$TMP/vault"
mkdir -p "$VAULT/wiki"
echo '# Index' > "$VAULT/wiki/index.md"

x3vault init --vault "$VAULT" >/dev/null
x3vault build --vault "$VAULT" >/dev/null
x3vault doctor --vault "$VAULT" >/dev/null
x3vault version | grep -q x3vault

BUILD_CURRENT="$(dirname "$VAULT")/ereader/build/current"
test -f "$BUILD_CURRENT/wiki/index.md"
test -f "$BUILD_CURRENT/build.manifest"

echo "smoke test ok: build output at $BUILD_CURRENT"
