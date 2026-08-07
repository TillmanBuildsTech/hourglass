#!/bin/sh
# Hourglass installer — for distros without a .deb/.rpm (or for trying it
# without packages). Prefer the packages where available:
#
#   Debian/Ubuntu:   dpkg -i hourglass_linux_amd64.deb
#   Fedora/RHEL:     rpm -i hourglass_linux_amd64.rpm
#   macOS:           brew install TillmanBuildsTech/tap/hourglass
#
# Usage:
#   curl -fsSL https://github.com/TillmanBuildsTech/hourglass/releases/latest/download/install.sh | sh
#   # or pin a version:
#   HOURGLASS_VERSION=0.10.0 curl -fsSL ... | sh
set -e

# --- detect OS + arch -------------------------------------------------------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# --- resolve version --------------------------------------------------------
VERSION="${HOURGLASS_VERSION:-}"
if [ -z "$VERSION" ]; then
    VERSION="$(curl -fsSL https://api.github.com/repos/TillmanBuildsTech/hourglass/releases/latest \
        | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"v\(.*\)"/\1/')"
fi
[ -z "$VERSION" ] && { echo "Could not determine latest version" >&2; exit 1; }

echo "Installing Hourglass v$VERSION ($OS/$ARCH)..."

# --- download + verify ------------------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
URL="https://github.com/TillmanBuildsTech/hourglass/releases/download/v${VERSION}/hourglass_${OS}_${ARCH}.tar.gz"
curl -fsSL "$URL" -o "$TMP/hourglass.tar.gz"
curl -fsSL "https://github.com/TillmanBuildsTech/hourglass/releases/download/v${VERSION}/checksums.txt" -o "$TMP/checksums.txt"

# verify sha256 (macOS: use shasum -a 256)
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP" && sha256sum -c checksums.txt --ignore-missing 2>/dev/null | grep -q "hourglass_${OS}_${ARCH}.tar.gz: OK") \
        || { echo "Checksum verification failed" >&2; exit 1; }
elif command -v shasum >/dev/null 2>&1; then
    EXPECTED="$(grep "hourglass_${OS}_${ARCH}.tar.gz" "$TMP/checksums.txt" | awk '{print $1}')"
    ACTUAL="$(shasum -a 256 "$TMP/hourglass.tar.gz" | awk '{print $1}')"
    [ "$EXPECTED" = "$ACTUAL" ] || { echo "Checksum verification failed" >&2; exit 1; }
else
    echo "Warning: no sha256sum/shasum found — skipping checksum verification" >&2
fi

tar -xzf "$TMP/hourglass.tar.gz" -C "$TMP"

# --- install binary ---------------------------------------------------------
DEST="${HOURGLASS_PREFIX:-/usr/local}"
install -m 0755 "$TMP/hourglass" "$DEST/bin/hourglass" 2>/dev/null \
    || { echo "Need write access to $DEST/bin — retry with sudo" >&2; exit 1; }
echo "Installed: $DEST/bin/hourglass"

# --- Linux: optional systemd service ----------------------------------------
if [ "$OS" = "linux" ] && command -v systemctl >/dev/null 2>&1 && [ "$(id -u)" = "0" ]; then
    CONF=/etc/hourglass.env
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    if [ ! -f "$CONF" ]; then
        if command -v openssl >/dev/null 2>&1; then
            PASSWORD="$(openssl rand -hex 8)"
        else
            PASSWORD="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
        fi
        ENV_TEMPLATE="$SCRIPT_DIR/../packaging/hourglass.env"
        [ -f "$ENV_TEMPLATE" ] || curl -fsSL "https://raw.githubusercontent.com/TillmanBuildsTech/hourglass/v${VERSION}/packaging/hourglass.env" -o /tmp/hourglass.env.tpl
        sed "s/__HOURGLASS_PASSWORD__/$PASSWORD/" "${ENV_TEMPLATE:-/tmp/hourglass.env.tpl}" > "$CONF"
        chmod 600 "$CONF"
        echo ""
        echo "Hourglass credentials: admin / $PASSWORD (saved in $CONF)"
        echo ""
    fi
    # install unit file (from the repo checkout when present, else fetch)
    UNIT_SRC="$SCRIPT_DIR/../packaging/hourglass.service"
    [ -f "$UNIT_SRC" ] || curl -fsSL "https://raw.githubusercontent.com/TillmanBuildsTech/hourglass/v${VERSION}/packaging/hourglass.service" -o /tmp/hourglass.service
    install -m 0644 "${UNIT_SRC:-/tmp/hourglass.service}" /usr/lib/systemd/system/hourglass.service
    # Optional: run the service as a non-root user (manages that user's
    # crontab) instead of the root default. mkdir -p + heredoc is used so the
    # drop-in is the ONLY place that sets User/Group, leaving the packaged
    # unit untouched and upgrade-safe.
    if [ -n "${HOURGLASS_USER:-}" ]; then
        mkdir -p /etc/systemd/system/hourglass.service.d
        cat > /etc/systemd/system/hourglass.service.d/user.conf <<EOF
[Service]
User=${HOURGLASS_USER}
Group=${HOURGLASS_USER}
EOF
        echo "Service runs as user: ${HOURGLASS_USER}"
    fi
    systemctl daemon-reload
    systemctl enable --now hourglass.service
    echo "Service started: systemctl status hourglass"
else
    echo ""
    echo "Run it with:  hourglass"
    echo "Then open http://hourglass.local:8080 from any device on this LAN"
    echo "(or http://localhost:8080 on this machine). First run generates a"
    echo "password and prints the login — saved in ~/.hourglass/auth.env."
    if [ "$OS" = "darwin" ]; then
        echo "Run as a service:  brew install TillmanBuildsTech/tap/hourglass && brew services start hourglass"
    fi
fi

echo "Done."
