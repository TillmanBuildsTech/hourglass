// Playwright end-to-end tests for the Hourglass login flow (auth enabled).
//
// The main suite (hourglass.spec.js) runs against a no-auth instance. This
// spec spawns its OWN file-backed instance with HOURGLASS_AUTH_USER/PASS set
// on a separate port, so it can verify the in-app login view end to end:
// no native browser auth prompt, wrong-password error, sign-in, the
// signed-in user chip in the sidebar footer, and logout revocation.
const { test, expect } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const BIN = process.env.HG_BIN || path.join(__dirname, '..', 'hourglass');
const PORT = process.env.HG_AUTH_PORT || '18082';
const BASE = `http://127.0.0.1:${PORT}`;

const USER = 'admin';
const PASS = 'testpass123';

let server;
let scratch;

test.beforeAll(async () => {
  scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'hourglass-auth-e2e-'));
  server = spawn(BIN, [], {
    env: {
      ...process.env,
      HOURGLASS_BIND: `127.0.0.1:${PORT}`,
      HOURGLASS_AUTH_USER: USER,
      HOURGLASS_AUTH_PASS: PASS,
      HOURGLASS_CRONTAB_FILE: path.join(scratch, 'crontab.txt'),
      HOME: scratch,
      HOURGLASS_MDNS: '0',
      HOURGLASS_TLS: 'off',
    },
    stdio: 'ignore',
  });

  // Wait for readiness (the public /api/version answers without auth).
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(`${BASE}/api/version`);
      if (res.ok) return;
    } catch (_) { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error('auth test instance did not start');
});

test.afterAll(() => {
  if (server) server.kill();
});

test.describe('Hourglass auth (login view)', () => {
  test('replaces the native auth prompt with the in-app login view', async ({ page }) => {
    // page.goto succeeding at all proves there is no native Basic Auth
    // dialog blocking the navigation — the shell is served publicly and the
    // app gates on /api/auth/me.
    await page.goto(BASE);
    await expect(page).toHaveTitle('Hourglass — Crontab Manager');
    await expect(page.locator('#view-login')).toBeVisible();
    await expect(page.locator('#login-username')).toBeVisible();
    await expect(page.locator('#login-submit')).toHaveText('Sign In');
    // No data behind the login view yet: the jobs table never left its
    // initial "Loading…" placeholder and the signed-in chip is hidden.
    await expect(page.locator('#auth-footer')).toBeHidden();
    await expect(page.locator('#jobs-table')).toContainText('Loading…');
  });

  test('shows an inline error for a wrong password', async ({ page }) => {
    await page.goto(BASE);
    await page.fill('#login-username', USER);
    await page.fill('#login-password', 'wrong-password');
    await page.click('#login-submit');

    await expect(page.locator('#login-error')).toBeVisible();
    await expect(page.locator('#login-error')).toContainText('Invalid username or password');
    await expect(page.locator('#view-login')).toBeVisible();
  });

  test('signs in, shows the user chip bottom-left, and persists the session', async ({ page }) => {
    await page.goto(BASE);
    await page.fill('#login-username', USER);
    await page.fill('#login-password', PASS);
    await page.click('#login-submit');

    // App is reachable and the login view is gone.
    await expect(page.locator('#view-login')).toBeHidden();
    await expect(page.getByRole('heading', { name: 'Cron Jobs' })).toBeVisible();
    await expect(page.locator('#jobs-table .empty-cell')).toContainText('No cron jobs yet');

    // Signed-in user chip in the sidebar footer (bottom-left of the page).
    await expect(page.locator('#auth-footer')).toBeVisible();
    await expect(page.locator('#auth-username')).toHaveText(USER);
    await expect(page.locator('#auth-avatar')).toHaveText('A');

    // Session cookie survives a full reload (no re-login prompt).
    await page.reload();
    await expect(page.locator('#view-login')).toBeHidden();
    await expect(page.locator('#auth-username')).toHaveText(USER);
  });

  test('logs out, revokes the session, and returns to the login view', async ({ page }) => {
    await page.goto(BASE);
    await page.fill('#login-username', USER);
    await page.fill('#login-password', PASS);
    await page.click('#login-submit');
    await expect(page.locator('#view-login')).toBeHidden();

    await page.click('#logout-btn');
    await expect(page.locator('#view-login')).toBeVisible();
    await expect(page.locator('#auth-footer')).toBeHidden();

    // The revoked cookie no longer authenticates: API calls return 401.
    const resp = await page.request.get(`${BASE}/api/cron`);
    expect(resp.status()).toBe(401);
  });
});
