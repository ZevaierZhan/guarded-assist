#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
export CGO_ENABLED=0 GOPROXY=off
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assist.exe ./cmd/assist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assist-server.exe ./cmd/assist-server
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assistctl.exe ./cmd/assistctl
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assist-linux-amd64 ./cmd/assist
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assist-server-linux-amd64 ./cmd/assist-server
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assistctl-linux-amd64 ./cmd/assistctl
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/assist-darwin-arm64 ./cmd/assist
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/assist-darwin-amd64 ./cmd/assist
