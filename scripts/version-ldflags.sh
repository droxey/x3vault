#!/usr/bin/env bash
# Source from other scripts to set LDFLAGS for reproducible x3vault builds.
VERSION="${VERSION:-$(git -C "$(dirname "$0")/.." describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$(dirname "$0")/.." rev-parse --short HEAD 2>/dev/null || echo none)}"
LDFLAGS="-X github.com/droxey/x3vault/internal/cli.Version=${VERSION} -X github.com/droxey/x3vault/internal/cli.Commit=${COMMIT}"
