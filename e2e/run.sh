#!/usr/bin/env bash
# Builds an isolated Hourglass binary and runs the Playwright E2E suite
# against it. The instance is file-backed (HOURGLASS_CRONTAB_FILE + scratch
# HOME) so real cron jobs are never touched.
set -euo pipefail

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$E2E_DIR/.." && pwd)"

# ── 1. Rebuild the embedded frontend (kept in dist/) from src/ ────────────
(cd "$ROOT/ui" && npm run build)

# ── 2. Scratch dir: isolated HOME + crontab file ──────────────────────────
export HG_TEST_HOME="$(mktemp -d /tmp/hourglass-e2e.XXXXXX)"
export HG_CRONTAB="$HG_TEST_HOME/crontab.txt"
export HG_HOST="${HG_HOST:-127.0.0.1}"
export HG_PORT="${HG_PORT:-18080}"

# ── 3. Build the test binary from current source ──────────────────────────
export HG_BIN="$HG_TEST_HOME/hourglass"
(cd "$ROOT" && go build -o "$HG_BIN" .)

# ── 4. Point at the locally cached Chromium (no re-download) ──────────────
export HG_CHROMIUM="${HG_CHROMIUM:-}"
if [ -z "$HG_CHROMIUM" ]; then
  LATEST="$(ls -1d /root/.cache/ms-playwright/chromium-*/chrome-linux*/chrome 2>/dev/null | tail -1 || true)"
  [ -n "$LATEST" ] && HG_CHROMIUM="$LATEST"
fi
export HG_CHROMIUM

echo "== running Playwright E2E ($HG_TEST_HOME) =="
(cd "$E2E_DIR" && npx playwright test "$@")
