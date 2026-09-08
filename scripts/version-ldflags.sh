#!/usr/bin/env bash
# Source from Bash to set version metadata for x3vault builds.
X3VAULT_VERSION_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-$(git -C "$X3VAULT_VERSION_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$X3VAULT_VERSION_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)}"
LDFLAGS="-X github.com/droxey/x3vault/internal/cli.Version=${VERSION} -X github.com/droxey/x3vault/internal/cli.Commit=${COMMIT}"
