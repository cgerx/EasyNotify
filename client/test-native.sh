#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p client/build
swiftc -swift-version 5 -framework UserNotifications client/Sources/Store.swift client/test/StoreTests.swift -o client/build/StoreTests
node client/test-native.mjs
