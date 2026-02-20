/**
 * Real-Time Event Lifecycle Test
 *
 * Validates that SSE events flow from the Hub API through to the browser UI.
 * Uses Playwright to hold a browser open on the grove detail page, then
 * exercises the full agent lifecycle via API calls (create, status update,
 * delete), taking screenshots at each step to verify the UI updates
 * dynamically without page reload.
 *
 * Prerequisites:
 *   - scion server running: scion server start --enable-hub --enable-web
 *       --enable-runtime-broker --dev-auth --web-assets-dir ./web/dist/client
 *   - Web assets built: cd web && npm run build
 *   - A grove and broker-provider link must already exist (see setup below)
 *   - Playwright + Chromium: cd /tmp && npm install playwright
 *
 * Usage:
 *   GROVE_ID=<uuid> TOKEN=<dev-token> node realtime-lifecycle-test.js
 *
 * Environment variables:
 *   GROVE_ID  - UUID of an existing grove (required)
 *   TOKEN     - Dev auth token from server startup logs (required)
 *   BASE      - Server base URL (default: http://localhost:8080)
 *   CHROMIUM  - Path to chromium binary (default: /usr/bin/chromium)
 *   OUT_DIR   - Screenshot output directory (default: /tmp)
 */
const { chromium } = require('playwright');

const GROVE_ID = process.env.GROVE_ID || '';
const TOKEN = process.env.TOKEN || '';
const BASE = process.env.BASE || 'http://localhost:8080';
const CHROMIUM_PATH = process.env.CHROMIUM || '/usr/bin/chromium';
const OUT_DIR = process.env.OUT_DIR || '/tmp';

if (!GROVE_ID || !TOKEN) {
  console.error('Usage: GROVE_ID=<uuid> TOKEN=<dev-token> node realtime-lifecycle-test.js');
  process.exit(1);
}

async function run() {
  const browser = await chromium.launch({
    executablePath: CHROMIUM_PATH,
    args: ['--no-sandbox', '--disable-setuid-sandbox'],
  });
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();

  // Track console messages (SSE connection state, errors)
  page.on('console', (msg) => {
    const text = msg.text();
    if (
      text.includes('[SSE') ||
      text.includes('Scion') ||
      text.includes('agent') ||
      text.includes('update')
    ) {
      console.log(`[CONSOLE ${msg.type()}] ${text}`);
    }
  });
  page.on('pageerror', (err) => console.log(`[PAGE ERROR] ${err.message}`));

  page.on('response', (resp) => {
    const url = resp.url();
    if (url.includes('/events') || url.includes('/api/')) {
      console.log(`[HTTP ${resp.status()}] ${url.substring(0, 120)}`);
    }
  });

  // Step 1: Navigate to the grove detail page
  console.log('\n=== STEP 1: Navigate to grove detail page ===');
  await page.goto(`${BASE}/groves/${GROVE_ID}`, {
    waitUntil: 'domcontentloaded',
    timeout: 15000,
  });
  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${OUT_DIR}/rt-01-grove-empty.png`, fullPage: false });
  console.log(`Screenshot: ${OUT_DIR}/rt-01-grove-empty.png`);

  // Step 2: Create an agent via API
  console.log('\n=== STEP 2: Create agent via API ===');
  const createResp = await fetch(`${BASE}/api/v1/agents`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      name: `rt-agent-${Date.now()}`,
      groveId: GROVE_ID,
      provisionOnly: true,
    }),
  });
  const createResult = await createResp.json();
  const agentId = createResult.agent?.id || createResult.id;
  console.log('Created agent ID:', agentId);
  console.log('Agent status:', createResult.agent?.status || createResult.status);

  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${OUT_DIR}/rt-02-agent-created.png`, fullPage: false });
  console.log(`Screenshot: ${OUT_DIR}/rt-02-agent-created.png`);

  if (!agentId) {
    console.log('ERROR: No agent ID returned. Response:', JSON.stringify(createResult));
    await browser.close();
    process.exit(1);
  }

  // Step 3: Update agent status to running (POST, not PATCH)
  console.log('\n=== STEP 3: Update agent status to running ===');
  const statusResp = await fetch(`${BASE}/api/v1/agents/${agentId}/status`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ status: 'running', sessionStatus: 'started' }),
  });
  console.log('Status update response:', statusResp.status);

  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${OUT_DIR}/rt-03-agent-running.png`, fullPage: false });
  console.log(`Screenshot: ${OUT_DIR}/rt-03-agent-running.png`);

  // Step 4: Update status to stopped
  console.log('\n=== STEP 4: Update agent status to stopped ===');
  const stopResp = await fetch(`${BASE}/api/v1/agents/${agentId}/status`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ status: 'stopped', sessionStatus: 'completed' }),
  });
  console.log('Stop response:', stopResp.status);

  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${OUT_DIR}/rt-04-agent-stopped.png`, fullPage: false });
  console.log(`Screenshot: ${OUT_DIR}/rt-04-agent-stopped.png`);

  // Step 5: Delete agent
  console.log('\n=== STEP 5: Delete agent ===');
  const deleteResp = await fetch(`${BASE}/api/v1/agents/${agentId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${TOKEN}` },
  });
  console.log('Delete response:', deleteResp.status);

  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${OUT_DIR}/rt-05-agent-deleted.png`, fullPage: false });
  console.log(`Screenshot: ${OUT_DIR}/rt-05-agent-deleted.png`);

  await browser.close();
  console.log('\n=== Test complete ===');
}

run().catch((err) => {
  console.error('Test failed:', err);
  process.exit(1);
});
