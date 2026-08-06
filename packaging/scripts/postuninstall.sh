#!/bin/sh
# Hourglass package postuninstall — reload systemd after the unit file is gone.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

exit 0
