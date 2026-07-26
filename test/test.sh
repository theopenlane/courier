#!/bin/bash

# Smoke test for the courier binary, used locally and as the brew tap test
# to confirm an installed binary runs and its core commands respond

set -euo pipefail

BINARY="${COURIER_BIN:-./courier}"

echo "using binary: $BINARY"

echo "==> courier version"
"$BINARY" version

echo "==> courier version --short"
"$BINARY" version --short

echo "==> courier --help"
"$BINARY" --help > /dev/null

for cmd in pull fmt plan apply; do
    echo "==> courier $cmd --help"
    "$BINARY" "$cmd" --help > /dev/null
done

# fmt against an empty workspace requires no API access and must succeed
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "==> courier fmt (empty workspace)"
"$BINARY" fmt --dir "$WORKDIR"

echo "all smoke tests passed"
