#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
APP="${APP_OUTPUT:-client/build/EasyNotify.app}"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
architectures="$(uname -m)"
if [[ "${1:-}" == "--universal" ]]; then architectures="arm64 x86_64"; fi
mkdir -p client/build
for arch in $architectures; do
  swiftc -swift-version 5 -O -target "$arch-apple-macosx13.0" \
    -debug-prefix-map "$(pwd)=/EasyNotify" \
    -framework AppKit -framework SwiftUI -framework WebKit -framework UserNotifications -framework ServiceManagement \
    client/Sources/*.swift -o "client/build/EasyNotify-$arch"
done
if [[ "${1:-}" == "--universal" ]]; then
  lipo -create client/build/EasyNotify-arm64 client/build/EasyNotify-x86_64 -output "$APP/Contents/MacOS/EasyNotify"
else
  cp "client/build/EasyNotify-$architectures" "$APP/Contents/MacOS/EasyNotify"
fi
bash client/build-icon.sh
cp client/Resources/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"
cp client/Resources/Info.plist "$APP/Contents/Info.plist"
cp client/Resources/reader.html "$APP/Contents/Resources/reader.html"
cp node_modules/marked/lib/marked.umd.js "$APP/Contents/Resources/marked.js"
cp node_modules/dompurify/dist/purify.min.js "$APP/Contents/Resources/purify.js"
cp node_modules/marked/LICENSE.md "$APP/Contents/Resources/marked-LICENSE.md"
cp node_modules/dompurify/LICENSE "$APP/Contents/Resources/dompurify-LICENSE"
codesign --force --deep --sign - "$APP"
echo "Built $APP"
