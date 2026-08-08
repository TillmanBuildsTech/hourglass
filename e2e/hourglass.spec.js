// Playwright end-to-end tests for the Hourglass web UI.
//
// These run against an isolated, file-backed instance (see playwright.config.js)
// so they exercise the real backend/API + frontend without touching real cron
// jobs. Because the backend is single-admin, tests are serial (workers:1) and
// each uses unique job names to stay independent.
const { test, expect } = require('@playwright/test');
const fs = require('fs');
const path = require('path');

const HOST = process.env.HG_HOST || '127.0.0.1';
const PORT = process.env.HG_PORT || '18080';
// Read VERSION dynamically so the assertion never goes stale after a bump.
const VERSION = fs.readFileSync(path.join(__dirname, '..', 'VERSION'), 'utf8').trim();

async function addJob(page, { schedule, command, comment }) {
  await page.getByRole('button', { name: 'Add New Job' }).click();
  await page.fill('#schedule-input', schedule);
  await page.fill('#command-input', command);
  await page.fill('#comment-input', comment);
  await page.click('#submit-btn');
}

test.describe('Hourglass UI', () => {
  test('loads the app and shows version + empty cron list (loopback, no auth)', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle('Hourglass — Crontab Manager');
    // Match whatever version the binary reports, so a VERSION bump doesn't
    // break this test.
    const v = await page.evaluate(() => fetch('/api/version').then(r => r.json()));
    await expect(page.locator('#app-version')).toHaveText(`v${v.version}`);
    await expect(page.getByRole('heading', { name: 'Cron Jobs' })).toBeVisible();
    await expect(page.locator('#jobs-table .empty-cell')).toContainText('No cron jobs yet');
  });

  test('sidebar keeps its full pre-removal structure without the local-only note', async ({ page }) => {
    await page.goto('/');

    // The removed info note (and its CSS class) must not exist at all.
    await expect(page.locator('#local-only-message')).toHaveCount(0);
    await expect(page.locator('.info-note')).toHaveCount(0);

    // Regression lock for the sidebar-layout breaks: the connection manager
    // sections, ONE divider between them and the Help / View Logs links, ONE
    // divider above the auth footer, and the auth footer must all remain
    // direct children of the sidebar in the original order — no stray </div>
    // may shift or re-parent them, and no extra dividers may creep back in.
    const kids = await page.locator('#connections-panel > *').evaluateAll(
      els => els.map(el => el.id || el.className)
    );
    expect(kids).toEqual([
      'sidebar-top',
      'sidebar-section',
      'sidebar-section',
      'divider',
      'sidebar-section',
      'sidebar-section',
      'connections-close',
      'add-connection-form',
      'auth-footer',
    ]);
    await expect(page.locator('#connections-panel .divider')).toHaveCount(2);
  });

  test('shows the TillmanBuildsTech copyright footer at the bottom of the page', async ({ page }) => {
    await page.goto('/');

    const footer = page.locator('.app-footer');
    await expect(footer).toBeVisible();
    await expect(footer).toContainText('©');
    await expect(footer).toContainText('TillmanBuildsTech.com');
    await expect(footer.locator('a[href="https://tillmanbuildstech.com"]')).toHaveCount(1);
  });

  test('theme toggle shows the action icon: moon in light mode, sun in dark mode', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('hg-theme', 'light'));
    await page.goto('/');

    const toggle = page.locator('#theme-toggle');
    // Light mode: the moon icon promises "switch to dark".
    await expect(page.locator('#icon-moon')).toBeVisible();
    await expect(page.locator('#icon-sun')).toBeHidden();
    await expect(toggle).toHaveAttribute('aria-label', 'Switch to dark mode');

    await toggle.click();

    // Dark mode: the sun icon promises "switch to light".
    await expect(page.locator('#icon-sun')).toBeVisible();
    await expect(page.locator('#icon-moon')).toBeHidden();
    await expect(toggle).toHaveAttribute('aria-label', 'Switch to light mode');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });

  test('adds a job and it appears with Never status', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '0 9 * * *', command: 'echo hello', comment: 'My Backup' });

    const row = page.locator('tr.job-row', { hasText: 'My Backup' });
    await expect(row).toBeVisible();
    await expect(row.locator('.job-sched')).toHaveText('0 9 * * *');
    await expect(row.locator('.job-lastrun')).toHaveText('Never');
    await expect(row.locator('.job-status .status-badge')).toHaveText('—');
  });

  test('Run now populates LastRun + success status (execute-for-history fix)', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: 'echo hello', comment: 'Runnable' });

    const row = page.locator('tr.job-row', { hasText: 'Runnable' });
    await row.locator('button[aria-label="Run now"]').click();
    await expect(row.locator('.job-status .status-badge')).toHaveText('✓');
    await expect(row.locator('.job-lastrun')).not.toHaveText('Never');
  });

  test('shows a loading bar on the row while the run request is pending', async ({ page }) => {
    await page.goto('/');
    // A slow command keeps /api/cron/execute pending long enough to observe
    // the row-level loading bar (gold→green bar flush to the row bottom).
    await addJob(page, { schedule: '* * * * *', command: 'sleep 2', comment: 'Slowpoke' });

    const row = page.locator('tr.job-row', { hasText: 'Slowpoke' });
    await row.locator('button[aria-label="Run now"]').click();

    // While the request is in flight: the row is marked running, the bar is
    // rendered, and the play button is disabled (double-click guard).
    await expect(row).toHaveClass(/running/);
    await expect(row.locator('.job-run-bar')).toBeVisible();
    await expect(row.locator('button[aria-label="Run now"]')).toBeDisabled();

    // Once the request settles: bar gone, button re-enabled, status recorded.
    await expect(row).not.toHaveClass(/running/);
    await expect(row.locator('.job-run-bar')).toHaveCount(0);
    await expect(row.locator('button[aria-label="Run now"]')).toBeEnabled();
    await expect(row.locator('.job-status .status-badge')).toHaveText('✓');
  });

  test('Run now of a failing command shows failure status', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: '/bin/false', comment: 'Fails' });

    let row = page.locator('tr.job-row', { hasText: 'Fails' });
    await row.locator('button[aria-label="Run now"]').click();
    // The run returns an error (non-zero exit) so the UI doesn't reload itself;
    // reload for fresh data, then the recorded failure shows as ✗.
    await page.reload();
    row = page.locator('tr.job-row', { hasText: 'Fails' });
    await expect(row.locator('.job-status .status-badge')).toHaveText('✗');
    await expect(row.locator('.job-lastrun')).not.toHaveText('Never');
  });

  test('toggles a job disabled then enabled', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: 'echo hi', comment: 'ToggleMe' });

    let row = page.locator('tr.job-row', { hasText: 'ToggleMe' });
    await row.locator('button[aria-label="Disable"]').click();
    await expect(row).toContainText('⏸ Disabled');
    await expect(row.locator('button[aria-label="Run now"]')).toHaveCount(0);

    await row.locator('button[aria-label="Enable"]').click();
    await expect(row).not.toContainText('⏸ Disabled');
    await expect(row.locator('button[aria-label="Run now"]')).toHaveCount(1);
  });

  test('edits a job', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: 'echo old', comment: 'Old Name' });

    const row = page.locator('tr.job-row', { hasText: 'Old Name' });
    await row.locator('button[aria-label="Edit"]').click();
    await expect(page.locator('#form-title')).toHaveText('Update Job');
    await expect(page.locator('#schedule-input')).toHaveValue('* * * * *');

    await page.fill('#schedule-input', '0 1 * * *');
    await page.fill('#comment-input', 'New Name');
    await page.click('#submit-btn');

    const newRow = page.locator('tr.job-row', { hasText: 'New Name' });
    await expect(newRow).toBeVisible();
    await expect(newRow.locator('.job-sched')).toHaveText('0 1 * * *');
  });

  test('deletes a job after confirmation', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: 'echo bye', comment: 'Doomed' });

    const row = page.locator('tr.job-row', { hasText: 'Doomed' });
    await row.locator('button[aria-label="Delete"]').click();
    await expect(page.locator('#delete-modal')).toBeVisible();
    await page.click('#delete-confirm');

    await expect(page.locator('tr.job-row', { hasText: 'Doomed' })).toHaveCount(0);
  });

  test('views logs after a run, pointing at the isolated history log', async ({ page }) => {
    await page.goto('/');
    await addJob(page, { schedule: '* * * * *', command: 'echo logme', comment: 'Logger' });

    const row = page.locator('tr.job-row', { hasText: 'Logger' });
    await row.locator('button[aria-label="Run now"]').click();
    await page.click('#nav-logs');

    // History log records <millis>\t<exit>\t<base64(command)> on disk, but the
    // logs view decodes each record into a human-readable row — the decoded
    // command "echo logme" (base64 "ZWNobyBsb2dtZQ==") proves the run was
    // logged AND rendered readably.
    await expect(page.locator('#log-content')).toContainText('echo logme');
    await expect(page.locator('#log-path')).toContainText('.hourglass/history.log');
  });
});
