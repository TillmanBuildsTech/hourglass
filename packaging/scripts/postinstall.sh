#!/bin/sh
# Hourglass package postinstall — shared by .deb and .rpm (GoReleaser nfpms).
set -e

CONF=/etc/hourglass.env
SERVICE=hourglass.service

# First install only: generate /etc/hourglass.env with a random password and
# print the credentials so the admin can log in. Upgrades keep user edits.
if [ ! -f "$CONF" ]; then
    if command -v openssl >/dev/null 2>&1; then
        PASSWORD=$(openssl rand -hex 8)
    else
        PASSWORD=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
    fi
    sed "s/__HOURGLASS_PASSWORD__/$PASSWORD/" \
        /usr/share/hourglass/hourglass.env > "$CONF"
    chmod 600 "$CONF"
    echo ""
    echo "============================================================"
    echo "  Hourglass is installed and running."
    echo ""
    echo "  URL:      http://hourglass.local:8080  (or http://<this-host>:8080)"
    echo "  Username: admin"
    echo "  Password: $PASSWORD"
    echo ""
    echo "  Credentials are saved in $CONF"
    echo "============================================================"
    echo ""
fi

# Enable + start on systemd systems (the package targets systemd distros).
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable "$SERVICE" >/dev/null 2>&1 || true
    systemctl restart "$SERVICE" >/dev/null 2>&1 || true
fi

exit 0
