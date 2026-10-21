#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

echo "Checking Go formatting..."
if [[ -n "$(gofmt -l ./cmd)" ]]; then
  echo "The following Go files are not formatted with gofmt:"
  gofmt -l ./cmd
  exit 1
fi

echo "Formatting check passed."

echo "Running go vet..."
go vet ./...

echo "Running build checks for shipped binaries..."
go build -trimpath ./cmd/api
go build -trimpath ./cmd/mcp
