// Playwright test: jobs installed OUTSIDE Hourglass (plain system crontab
// lines) used to show "Never" forever, because only wrapped commands log
// executions. The app now wraps such jobs on first read so they start
// reporting LastRun/LastStatus — and preserves env lines (PATH=...) and
// header comments when it rewrites the crontab, so nothing breaks.
const { test, expect } = require('@playwright/test');
const fs = require('fs');
const path = require('path');

test('untracked jobs get wrapped on first view; env lines and comments survive', async ({ page }) => {
  // Seed the instance's file-backed crontab like a system-installed crontab.
  const crontab = process.env.HG_CRONTAB
    || path.join(process.env.HG_TEST_HOME || path.join(__dirname, '.scratch'), 'crontab.txt');
  fs.writeFileSync(crontab, [
    '# system-managed jobs',
    'PATH=/opt/custom/bin:/usr/bin:/bin',
    '0 6 * * * echo untracked-ran',
    '',
  ].join('\n'));

  await page.goto('/');

  // The untracked job appears with its plain command and "Never" status...
  const row = page.locator('tr.job-row', { hasText: 'untracked-ran' });
  await expect(row).toBeVisible();
  await expect(row.locator('.job-status .status-badge')).toHaveText('—');

  // ...and the crontab file has been rewritten: the job is now wrapped for
  // history tracking, while the env line and header comment are preserved.
  await expect.poll(() => fs.readFileSync(crontab, 'utf8')).toContain('history.log');
  const text = fs.readFileSync(crontab, 'utf8');
  expect(text).toContain('PATH=/opt/custom/bin:/usr/bin:/bin');
  expect(text).toContain('# system-managed jobs');
  expect(text).toContain('[[hg:');

  // Run now: the wrapped execution records a success, so LastRun/LastStatus
  // populate immediately (this was the whole point of auto-tracking).
  await row.locator('button[aria-label="Run now"]').click();
  await expect(row.locator('.job-status .status-badge')).toHaveText('✓');
  await expect(row.locator('.job-lastrun')).not.toHaveText('Never');
});
