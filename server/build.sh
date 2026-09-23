#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p server/build mcp/build
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o server/build/easynotify-server ./server/cmd/easynotify-server
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o mcp/build/easynotify-mcp ./mcp/cmd/easynotify-mcp
if [[ "${1:-}" == "--linux" ]]; then
  mkdir -p server/dist mcp/dist
  for target in amd64 arm64 armv7 armv6; do
    arch="$target"
    arm=""
    case "$target" in armv7) arch=arm; arm=7;; armv6) arch=arm; arm=6;; esac
    for component in server mcp; do
      CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOARM="$arm" go build -trimpath -ldflags='-s -w' \
        -o "$component/dist/easynotify-$component-linux-$target" "./$component/cmd/easynotify-$component"
    done
  done
fi
