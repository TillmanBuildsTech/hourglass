// Playwright tests for connection-swap correctness. These run against the
// isolated file-backed instance (no real SSH needed) and cover:
//   1. The response-ordering race: loadJobs() is triggered by the 5s poll AND
//      after every mutation/connection switch. When the active host is
//      SSH-remote a /api/cron response can take seconds; if it resolves AFTER
//      a newer request (e.g. right after switching back to local), the stale
//      response used to render last — showing the previous connection's jobs
//      until the next poll. The fix sequence-tags requests so only the latest
//      render wins.
//   2. Failed remote switch rollback: a switch to an unreachable host must not
//      leave the app claiming that host is active while still serving the old
//      one.
const { test, expect } = require('@playwright/test');

test('a stale in-flight cron response cannot overwrite a newer one', async ({ page }) => {
  await page.goto('/');

  // Seed a job via the API so the list has known content (fetch needs a real
  // page URL, so this runs after navigation).
  await page.evaluate(async () => {
    await fetch('/api/cron', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Schedule: '* * * * *', Command: 'echo race', Comment: 'RaceJob' }),
    });
  });
  await page.evaluate(() => loadJobs());
  await expect(page.locator('tr.job-row', { hasText: 'RaceJob' })).toBeVisible();

  // Intercept /api/cron: the FIRST response is delayed 1.5s and rewritten to
  // carry a stale marker; every later response passes through unchanged.
  let cronCount = 0;
  await page.route('**/api/cron', async (route) => {
    cronCount++;
    const resp = await route.fetch();
    let body = await resp.text();
    if (cronCount === 1) {
      body = body.replaceAll('RaceJob', 'StaleRaceJob');
      await new Promise((r) => setTimeout(r, 1500));
    }
    await route.fulfill({ response: resp, body });
  });

  // Kick off the slow (stale) load, then a fast one that supersedes it.
  await page.evaluate(() => loadJobs());
  await page.waitForTimeout(250);
  await page.evaluate(() => loadJobs());

  // The fresh response renders immediately...
  await expect(page.locator('tr.job-row', { hasText: 'RaceJob' })).toBeVisible();

  // ...and when the stale response finally lands it must be dropped, not
  // re-rendered over the fresh list.
  await page.waitForTimeout(1800);
  await expect(page.locator('tr.job-row', { hasText: 'RaceJob' })).toBeVisible();
  await expect(page.locator('tr.job-row', { hasText: 'StaleRaceJob' })).toHaveCount(0);
});

test('a failed remote switch rolls back and stays on the previous connection', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#connection-status')).toHaveText('Local');

  // Create a connection that fails fast (connection refused on port 1).
  await page.evaluate(async () => {
    await fetch('/api/connections', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: 'dead-conn', host: '127.0.0.1', port: 1,
        user: 'root', key_path: '/nonexistent', label: 'Dead',
      }),
    });
  });

  // Switching to it must fail...
  const status = await page.evaluate(async () => {
    const r = await fetch('/api/connections/active', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: 'dead-conn' }),
    });
    return r.status;
  });
  expect(status).toBe(500);

  // ...and roll back: the active connection is still local (GET returns null).
  const active = await page.evaluate(() =>
    fetch('/api/connections/active').then((r) => r.json())
  );
  expect(active).toBeNull();

  // The UI still identifies the local connection as active.
  await expect(page.locator('#connection-status')).toHaveText('Local');
});
