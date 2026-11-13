import { readFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const requiredUtilities = ['.grid{', '.flex{', '.items-center{', '.px-5{', '.py-4{'];

export function verifyBuildCSS(outputDirectory) {
  const root = path.resolve(outputDirectory);
  const html = readFileSync(path.join(root, 'index.html'), 'utf8');
  const stylesheets = [];
  for (const [tag] of html.matchAll(/<link\b[^>]*>/gi)) {
    const attributes = Object.fromEntries(
      [...tag.matchAll(/([\w-]+)\s*=\s*["']([^"']*)["']/g)]
        .map(([, name, value]) => [name.toLowerCase(), value]),
    );
    if (!attributes.rel?.split(/\s+/).includes('stylesheet') || !attributes.href) continue;
    const assetURL = new URL(attributes.href, 'https://build.invalid/');
    if (assetURL.origin !== 'https://build.invalid') throw new Error('Entry stylesheet must be local');
    const assetPath = path.resolve(root, `.${decodeURIComponent(assetURL.pathname)}`);
    const relativePath = path.relative(root, assetPath);
    if (relativePath.startsWith('..') || path.isAbsolute(relativePath)) throw new Error('Invalid stylesheet path');
    const css = readFileSync(assetPath, 'utf8');
    if (!css.trim()) throw new Error(`Empty entry stylesheet: ${attributes.href}`);
    stylesheets.push(css);
  }
  if (!stylesheets.length) throw new Error('No entry stylesheets found in index.html');
  const css = stylesheets.join('\n');
  for (const utility of requiredUtilities) {
    if (!css.includes(utility)) throw new Error(`Missing required CSS utility: ${utility}`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    if (!process.argv[2]) throw new Error('Usage: node verify-build-css.mjs <output-directory>');
    verifyBuildCSS(process.argv[2]);
    console.log('Entry stylesheet utilities verified');
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
