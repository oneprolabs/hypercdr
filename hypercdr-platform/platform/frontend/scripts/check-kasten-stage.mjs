// Quick visual / data check for the kasten-io "stage regression" bug.
//
// Usage (on host, with 3002 and 18080 reachable):
//   PLAYWRIGHT_BROWSERS_PATH=/data/software/ms-playwright \
//   node scripts/check-kasten-stage.mjs
//
// Output: prints current API state of kasten-io on the default cluster,
// navigates to /clusters and /applications in the UI, takes screenshots,
// and reports where kasten-io appears (stage 1 / 2 / 3).
import { chromium } from '@playwright/test';
import fs from 'node:fs/promises';

const baseURL = process.env.PLATFORM_URL || 'http://127.0.0.1:3002';
const outDir = '/tmp/hypercdr-kasten-check';
const chromePath = '/data/software/ms-playwright/chromium-1223/chrome-linux64/chrome';

await fs.mkdir(outDir, { recursive: true });

// 1. Verify API state directly.
const apiRes = await fetch(`${baseURL.replace(/\/$/, '')}/api/v1/applications`);
const api = await apiRes.json();
const apps = (api.items || []).filter(a => a.namespace === 'kasten-io');
console.log(`API kasten-io count: ${apps.length}`);
for (const a of apps) {
  console.log(`  clusterId=${a.clusterId}  protectionStatus=${a.protectionStatus}  workloadCount=${a.workloadCount}  pvcCount=${a.pvcCount}`);
}
if (apps.length === 0 || !apps.some(a => a.protectionStatus === 'pending_protection')) {
  console.error('  !! EXPECTED at least one kasten-io with protectionStatus=pending_protection (stage 2)');
  process.exitCode = 1;
}

const browser = await chromium.launch({
  executablePath: chromePath,
  args: ['--no-sandbox', '--disable-crash-reporter', '--disable-crashpad'],
});
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await ctx.newPage();
page.on('console', msg => console.log(`[browser:${msg.type()}]`, msg.text()));

await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(800);
const sign = page.getByRole('button', { name: /sign in/i });
if (await sign.count()) await sign.first().click({ force: true });
await page.waitForTimeout(800);

// Application DR page
await page.getByRole('button', { name: /DR|Application DR/i }).first().click({ force: true }).catch(() => {});
await page.waitForTimeout(800);
// Try to land on /applications
const url1 = page.url();
console.log('after DR click, url=', url1);
await page.screenshot({ path: `${outDir}/01-dr-page.png`, fullPage: true });

// Wait a few seconds, then take another screenshot to confirm the stage didn't
// bounce back to stage 1 over a heartbeat cycle.
await page.waitForTimeout(15000);
await page.screenshot({ path: `${outDir}/02-dr-page-after-15s.png`, fullPage: true });

await page.waitForTimeout(15000);
await page.screenshot({ path: `${outDir}/03-dr-page-after-30s.png`, fullPage: true });

// Read DOM: locate kasten-io row, report its column values.
const rowInfo = await page.evaluate(() => {
  const rows = Array.from(document.querySelectorAll('tr, [role="row"]'));
  const matches = [];
  for (const r of rows) {
    if ((r.textContent || '').toLowerCase().includes('kasten-io')) {
      matches.push(r.textContent.replace(/\s+/g, ' ').trim().slice(0, 400));
    }
  }
  return matches;
});
console.log('UI rows mentioning kasten-io:');
for (const t of rowInfo) console.log(`  - ${t}`);

await browser.close();
console.log(`Screenshots saved to ${outDir}`);
