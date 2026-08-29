#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GO_DIR="$SCRIPT_DIR/go"

echo "Building math-viz-plugin in $GO_DIR ..."

cd "$GO_DIR"
rm -f plugin.wasm

docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  golang:latest \
  sh -c 'go clean -cache && go mod tidy && GOOS=wasip1 GOARCH=wasm go build -o plugin.wasm -buildmode=c-shared .'

if [ ! -f plugin.wasm ]; then
  echo "ERROR: plugin.wasm was not created"
  exit 1
fi

SIZE=$(du -h plugin.wasm | cut -f1)
echo "Build successful: $GO_DIR/plugin.wasm ($SIZE)"