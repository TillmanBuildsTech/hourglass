// Playwright capture of dark-mode UI screenshots for the README/docs.
//
// Run via scripts/screenshots.sh (or directly with the screenshots.config.js
// config) against an isolated, file-backed instance seeded with mock cron jobs
// and execution history — real cron jobs are never touched.
const { test } = require('@playwright/test');
const path = require('path');

const OUT = process.env.HG_SHOT_DIR || path.join(__dirname, '..', 'docs', 'screenshots');

test.describe('README/docs screenshots', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('dark-mode screenshots', async ({ page }) => {
    // Force dark theme regardless of OS preference or previously saved state.
    await page.addInitScript(() => localStorage.setItem('hg-theme', 'dark'));
    await page.emulateMedia({ colorScheme: 'dark' });

    // ── Main cron jobs view ────────────────────────────────────────────────
    await page.goto('/');
    await page.waitForSelector('tr.job-row');
    // Seeded history must render (✓ / ✗ status badges) before shooting.
    await page.waitForSelector('.status-badge.ok', { timeout: 10000 });
    await page.waitForTimeout(400); // let fonts / relative times settle
    await page.screenshot({ path: path.join(OUT, 'hourglass-dark.png'), fullPage: true });

    // ── Logs view ──────────────────────────────────────────────────────────
    await page.getByRole('button', { name: 'View Logs' }).click();
    await page.waitForSelector('#view-logs:not(.hidden)');
    // log-content starts as "Loading…", so wait until the seeded history
    // actually renders before shooting.
    await page.waitForFunction(() => {
      const el = document.getElementById('log-content');
      return el && el.textContent && !el.textContent.includes('Loading')
        && el.textContent.trim().length > 0;
    }, null, { timeout: 10000 });
    await page.waitForTimeout(400);
    await page.screenshot({ path: path.join(OUT, 'logs-dark.png'), fullPage: true });
  });
});
