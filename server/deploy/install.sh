#!/bin/sh
# Run as root in a directory containing the binary, config.json and service unit.
set -eu
if ! id easynotify >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin easynotify
fi
install -m 0755 easynotify-server /usr/local/bin/easynotify-server.new
mv /usr/local/bin/easynotify-server.new /usr/local/bin/easynotify-server
install -d -m 0750 -o root -g easynotify /etc/easynotify
# Preserve the installed key during upgrades.
if [ ! -f /etc/easynotify/config.json ]; then
  install -m 0640 -o root -g easynotify config.json /etc/easynotify/config.json
fi
install -m 0644 easynotify.service /etc/systemd/system/easynotify.service
systemctl daemon-reload
systemctl enable easynotify
systemctl restart easynotify
systemctl is-active easynotify
