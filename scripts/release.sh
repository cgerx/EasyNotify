#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
bash server/build.sh --linux
APP_OUTPUT="$stage/EasyNotify.app" bash client/build.sh --universal
codesign --verify --deep --strict "$stage/EasyNotify.app"
mkdir -p "$stage/licenses"
go list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}{{end}}' ./server/cmd/easynotify-server ./mcp/cmd/easynotify-mcp | sort -u > "$stage/modules.txt"
while IFS='|' read -r module directory; do
  [[ -n "$directory" ]] || continue
  for license in "$directory"/LICENSE* "$directory"/COPYING*; do
    [[ -f "$license" ]] || continue
    name="${module//\//_}-$(basename "$license")"
    cp "$license" "$stage/licenses/$name"
  done
done < "$stage/modules.txt"
for arch in amd64 arm64 armv7 armv6; do
  name="easynotify-server-linux-$arch"
  mkdir -p "$stage/$name"
  cp "server/dist/$name" "$stage/$name/easynotify-server"
  cp "mcp/dist/easynotify-mcp-linux-$arch" "$stage/$name/easynotify-mcp"
  cp server/deploy/install.sh server/deploy/easynotify.service README.md "$stage/$name/"
  cp -R "$stage/licenses" "$stage/$name/licenses"
  COPYFILE_DISABLE=1 tar --uid 0 --gid 0 --uname root --gname root -czf "dist/$name.tar.gz" -C "$stage" "$name"
done
ditto -c -k --norsrc --keepParent "$stage/EasyNotify.app" dist/EasyNotify-macos-universal.zip
(cd dist && shasum -a 256 ./*.zip ./*.tar.gz > SHA256SUMS)
