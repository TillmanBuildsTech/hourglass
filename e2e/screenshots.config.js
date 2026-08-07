// Playwright config for the README/docs screenshot capture only.
//
// Kept separate from playwright.config.js (testMatch: '**/*.spec.js') so the
// normal `e2e/run.sh` suite never picks up the screenshot job and clobbers
// docs/screenshots/. Same isolated instance machinery as the E2E suite:
// file-backed crontab + scratch HOME, booted by Playwright's webServer.
const base = require('./playwright.config.js');

module.exports = {
  ...base,
  testMatch: '**/screenshots.capture.js',
};
