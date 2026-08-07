// Playwright config for Hourglass E2E.
//
// The test suite runs against a completely isolated instance that is launched
// by Playwright's webServer. Isolation is achieved two ways:
//   - HOURGLASS_CRONTAB_FILE=<scratch crontab.txt>  → the backend reads/writes
//     a plain file instead of the real system crontab (cron.FileExecutor).
//   - HOME=<scratch home>                            → history.log and
//     connections.json live in the scratch dir, never the real $HOME.
//
// This means the suite can add/edit/delete/toggle/run jobs without ever
// touching production cron jobs.
const path = require('path');

const HOST = process.env.HG_HOST || '127.0.0.1';
const PORT = process.env.HG_PORT || '18080';
const TEST_HOME = process.env.HG_TEST_HOME || path.join(__dirname, '.scratch');
const CRONTAB = process.env.HG_CRONTAB || path.join(TEST_HOME, 'crontab.txt');
const BIN = process.env.HG_BIN || path.join(__dirname, '..', 'hourglass');
const CHROMIUM = process.env.HG_CHROMIUM || '';

module.exports = {
  testDir: __dirname,
  testMatch: '**/*.spec.js',
  timeout: 30000,
  retries: 0,
  workers: 1, // the backend is single-admin; keep tests serial
  use: {
    baseURL: `http://${HOST}:${PORT}`,
    launchOptions: CHROMIUM ? { executablePath: CHROMIUM } : {},
  },
  webServer: {
    command: `env HOURGLASS_BIND=${HOST}:${PORT} HOURGLASS_CRONTAB_FILE=${CRONTAB} HOME=${TEST_HOME} HOURGLASS_MDNS=0 ${BIN}`,
    url: `http://${HOST}:${PORT}/api/version`,
    reuseExistingServer: true,
    timeout: 30000,
  },
};
