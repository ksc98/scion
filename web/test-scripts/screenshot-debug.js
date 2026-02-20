/**
 * Debug Screenshot Tool
 *
 * Takes a screenshot of a URL while capturing console logs, network errors,
 * and response statuses. Useful for diagnosing why a page is blank or
 * misbehaving in a headless environment.
 *
 * Usage:
 *   node screenshot-debug.js [url] [output-path]
 *
 * Examples:
 *   node screenshot-debug.js http://localhost:8080/ /tmp/debug.png
 *   node screenshot-debug.js http://localhost:8080/groves/abc123 /tmp/grove.png
 *
 * Prerequisites:
 *   - Playwright + Chromium: cd /tmp && npm install playwright
 */
const { chromium } = require('playwright');

const CHROMIUM_PATH = process.env.CHROMIUM || '/usr/bin/chromium';

async function debug(url, outputPath) {
  const browser = await chromium.launch({
    executablePath: CHROMIUM_PATH,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  // Capture console messages
  page.on('console', (msg) => console.log(`[CONSOLE ${msg.type()}] ${msg.text()}`));
  page.on('pageerror', (err) => console.log(`[PAGE ERROR] ${err.message}`));

  // Capture network failures
  page.on('requestfailed', (req) =>
    console.log(`[NET FAIL] ${req.url()} - ${req.failure()?.errorText}`)
  );

  // Capture responses of interest (errors, assets, API, SSE)
  page.on('response', (resp) => {
    if (
      resp.status() >= 400 ||
      resp.url().includes('.js') ||
      resp.url().includes('.css') ||
      resp.url().includes('/events') ||
      resp.url().includes('/api/')
    ) {
      console.log(`[RESPONSE] ${resp.status()} ${resp.url()}`);
    }
  });

  await page
    .goto(url, { waitUntil: 'networkidle', timeout: 15000 })
    .catch((e) => console.log(`[NAV ERROR] ${e.message}`));
  await page.waitForTimeout(3000);

  // Print page HTML summary
  const html = await page.content();
  console.log(`\n[HTML length] ${html.length}`);
  console.log(`[HTML snippet] ${html.substring(0, 500)}`);

  await page.screenshot({ path: outputPath, fullPage: false });
  console.log(`\nScreenshot saved to ${outputPath}`);

  await browser.close();
}

const url = process.argv[2] || 'http://localhost:8080';
const output = process.argv[3] || '/tmp/screenshot-debug.png';
debug(url, output);
