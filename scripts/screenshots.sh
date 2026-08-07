#!/usr/bin/env bash
# Generates dark-mode UI screenshots for the README/docs using Playwright.
#
# Boots an isolated, file-backed Hourglass instance (mock crontab + seeded
# execution history) so real cron jobs are never touched. Output goes to
# docs/screenshots/ (override with $1).
#
# Usage:
#   bash scripts/screenshots.sh [output-dir]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
E2E_DIR="$ROOT/e2e"
OUT="${1:-$ROOT/docs/screenshots}"

# ── 1. Build the embedded frontend + the binary ───────────────────────────
(cd "$ROOT/ui" && npm run build)
export HG_TEST_HOME="$(mktemp -d /tmp/hourglass-shots.XXXXXX)"
export HG_CRONTAB="$HG_TEST_HOME/crontab.txt"
export HG_HOST="${HG_HOST:-127.0.0.1}"
export HG_PORT="${HG_PORT:-18081}"
export HG_BIN="$HG_TEST_HOME/hourglass"
(cd "$ROOT" && go build -o "$HG_BIN" .)

# ── 2. Seed a mock crontab (file-backed, never the system crontab) ────────
cat > "$HG_CRONTAB" <<'EOF'
*/5 * * * * /usr/local/bin/backup-db --all # Database backup
0 2 * * * /usr/local/bin/rotate-logs >/dev/null 2>&1 # Rotate application logs
*/15 * * * * /usr/local/bin/check-cert-expiry # SSL certificate expiry check
0 9 * * 1 /usr/local/bin/send-weekly-report # Weekly usage report email
30 4 * * * /usr/local/bin/cleanup-tmp # Clean up temp files
# 0 3 * * * /usr/local/bin/purge-cache # Purge stale cache files
EOF

# ── 3. Seed execution history so the table shows real LastRun/Status values
#      (<millis>\t<exit>\t<base64(cmd)> — command must match the job exactly)
mkdir -p "$HG_TEST_HOME/.hourglass"
HISTORY="$HG_TEST_HOME/.hourglass/history.log"
NOW_MS=$(( $(date +%s) * 1000 ))
history_line() { # command exit_code minutes_ago
  local b64
  b64="$(printf '%s' "$1" | base64 | tr -d '\n')"
  printf '%d\t%d\t%s\n' "$(( NOW_MS - $3 * 60000 ))" "$2" "$b64"
}
history_line "/usr/local/bin/backup-db --all"              0 12   >> "$HISTORY"
history_line "/usr/local/bin/check-cert-expiry"            1 45   >> "$HISTORY"
history_line "/usr/local/bin/rotate-logs >/dev/null 2>&1"  0 120  >> "$HISTORY"
history_line "/usr/local/bin/cleanup-tmp"                  0 1560 >> "$HISTORY"

# ── 4. Point at the locally cached Chromium (no re-download) ───────────────
export HG_CHROMIUM="${HG_CHROMIUM:-}"
if [ -z "$HG_CHROMIUM" ]; then
  LATEST="$(ls -1d /root/.cache/ms-playwright/chromium-*/chrome-linux*/chrome 2>/dev/null | tail -1 || true)"
  [ -n "$LATEST" ] && HG_CHROMIUM="$LATEST"
fi
export HG_CHROMIUM

# ── 5. Run the capture ────────────────────────────────────────────────────
mkdir -p "$OUT"
export HG_SHOT_DIR="$OUT"
echo "== capturing screenshots -> $OUT =="
(cd "$E2E_DIR" && npx playwright test --config=screenshots.config.js)
echo "Done:"
ls -la "$OUT"
