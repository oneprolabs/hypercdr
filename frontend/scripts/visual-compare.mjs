import { chromium } from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';

const prototypeBaseURL = process.env.PROTOTYPE_URL || 'http://127.0.0.1:3001';
const platformBaseURL = process.env.PLATFORM_URL || 'http://127.0.0.1:3002';
const outputDir = process.env.VISUAL_COMPARE_DIR || '/tmp/hypercdr-visual-diffs';
const defaultChromiumExecutable = '/data/software/ms-playwright/chromium-1223/chrome-linux64/chrome';
const chromiumExecutable = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || defaultChromiumExecutable;

delete process.env.DISPLAY;
delete process.env.WAYLAND_DISPLAY;
delete process.env.XAUTHORITY;

const routes = [
  { name: 'dashboard', navText: ['Dashboard'] },
  { name: 'applications', navText: ['Application DR', 'Applications', 'DR'] },
  { name: 'clusters', navText: ['Clusters'] },
  { name: 'storage', navText: ['Storage'] },
  { name: 'policies', navText: ['Policies'] },
  { name: 'restore-points', navText: ['Restore Points', 'Restore'] },
  { name: 'operations', navText: ['History', 'Operations'] },
];

const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'tablet', width: 1024, height: 900 },
];

async function waitForApp(page) {
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(800);
}

async function stabilizeVisuals(page) {
  await page.addStyleTag({
    content: `
      *, *::before, *::after {
        animation-duration: 0s !important;
        animation-delay: 0s !important;
        transition-duration: 0s !important;
        transition-delay: 0s !important;
        scroll-behavior: auto !important;
      }
    `,
  }).catch(() => {});
}

async function signIn(page, baseURL) {
  await page.goto(baseURL, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await waitForApp(page);
  await stabilizeVisuals(page);
  const signIn = page.getByRole('button', { name: /sign in/i });
  if (await signIn.count()) {
    await signIn.first().click({ force: true });
    await waitForApp(page);
    await stabilizeVisuals(page);
  }
}

async function navigateTo(page, route) {
  if (route.name === 'dashboard') {
    const dashboard = page.getByText(route.navText[0], { exact: true });
    if (await dashboard.count()) {
      await dashboard.first().click({ force: true }).catch(() => {});
      await waitForApp(page);
    }
    return;
  }

  for (const text of route.navText) {
    const navItem = page.getByText(text, { exact: true });
    if (await navItem.count()) {
      await navItem.first().click({ force: true });
      await waitForApp(page);
      return;
    }
  }

  await page.evaluate((viewName) => {
    window.location.hash = `view=${viewName}`;
  }, route.name === 'restore-points' ? 'restore_points' : route.name);
  await waitForApp(page);
}

async function capturePage(browser, baseURL, side, route, viewport) {
  const page = await browser.newPage({ viewport });
  await signIn(page, baseURL);
  await navigateTo(page, route);
  const filePath = path.join(outputDir, `${route.name}-${viewport.name}-${side}.png`);
  console.log(`Capturing ${side} ${route.name} ${viewport.name}`);
  await page.screenshot({ path: filePath, fullPage: false });
  await page.close();
  return filePath;
}

async function main() {
  await fs.mkdir(outputDir, { recursive: true });
  const launchOptions = {
    headless: true,
    args: [
      '--headless=new',
      '--disable-crash-reporter',
      '--disable-crashpad',
      '--no-sandbox',
      '--disable-dev-shm-usage',
      '--disable-gpu',
    ],
  };
  try {
    await fs.access(chromiumExecutable);
    launchOptions.executablePath = chromiumExecutable;
  } catch {
    // Fall back to Playwright-managed browser discovery.
  }
  const browser = await chromium.launch(launchOptions);
  const results = [];

  try {
    for (const viewport of viewports) {
      for (const route of routes) {
        const prototype = await capturePage(browser, prototypeBaseURL, 'prototype', route, viewport);
        const platform = await capturePage(browser, platformBaseURL, 'platform', route, viewport);
        results.push({ route: route.name, viewport: viewport.name, prototype, platform });
      }
    }
  } finally {
    await browser.close();
  }

  const reportPath = path.join(outputDir, 'report.json');
  await fs.writeFile(reportPath, JSON.stringify({
    prototypeBaseURL,
    platformBaseURL,
    generatedAt: new Date().toISOString(),
    results,
  }, null, 2));

  console.log(`Visual comparison screenshots written to ${outputDir}`);
  console.log(`Report: ${reportPath}`);
}

main().catch(error => {
  console.error(error);
  process.exit(1);
});
