import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { verifyBuildCSS } from './verify-build-css.mjs';

const utilities = '.grid{display:grid}.flex{display:flex}.items-center{align-items:center}.px-5{padding-inline:1.25rem}.py-4{padding-block:1rem}';

function fixture(context, html = '<link rel="stylesheet" crossorigin href="/assets/index-main.css">') {
  const directory = mkdtempSync(path.join(os.tmpdir(), 'hypercdr-build-css-'));
  context.after(() => rmSync(directory, { recursive: true, force: true }));
  mkdirSync(path.join(directory, 'assets'));
  writeFileSync(path.join(directory, 'index.html'), html);
  return directory;
}

test('entry CSS is checked even when a lazy stylesheet is created first', (context) => {
  const directory = fixture(context);
  writeFileSync(path.join(directory, 'assets/task-detail-loader.css'), '.loader{display:block}');
  writeFileSync(path.join(directory, 'assets/index-main.css'), utilities);
  assert.doesNotThrow(() => verifyBuildCSS(directory));
});

test('unreferenced CSS cannot hide a missing entry utility', (context) => {
  const directory = fixture(context);
  writeFileSync(path.join(directory, 'assets/index-main.css'), '.grid{display:grid}');
  writeFileSync(path.join(directory, 'assets/unused.css'), utilities);
  assert.throws(() => verifyBuildCSS(directory), /Missing required CSS utility: \.flex/);
});

test('all entry stylesheets are checked, including query strings and single quotes', (context) => {
  const directory = fixture(context, "<link href='/assets/base.css?v=1' rel='stylesheet'><link rel='stylesheet' href='/assets/utilities.css'>");
  writeFileSync(path.join(directory, 'assets/base.css'), '.base{display:block}');
  writeFileSync(path.join(directory, 'assets/utilities.css'), utilities);
  assert.doesNotThrow(() => verifyBuildCSS(directory));
});

test('missing, empty and absent entry stylesheets fail', (context) => {
  const directory = fixture(context);
  assert.throws(() => verifyBuildCSS(directory), /ENOENT/);
  writeFileSync(path.join(directory, 'assets/index-main.css'), '');
  assert.throws(() => verifyBuildCSS(directory), /Empty entry stylesheet/);
  writeFileSync(path.join(directory, 'index.html'), '<html></html>');
  assert.throws(() => verifyBuildCSS(directory), /No entry stylesheets/);
});

test('external stylesheets are rejected', (context) => {
  const directory = fixture(context, '<link rel="stylesheet" href="https://example.com/assets/index.css">');
  assert.throws(() => verifyBuildCSS(directory), /must be local/);
});
