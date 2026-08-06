#!/bin/sh
# Hourglass package preuninstall — stop + disable the service before removal.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop hourglass.service >/dev/null 2>&1 || true
    systemctl disable hourglass.service >/dev/null 2>&1 || true
fi

exit 0
