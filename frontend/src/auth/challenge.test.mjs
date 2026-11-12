import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { validateAuthChallenge } from './challenge.ts';

test('only an explicit server configuration may enable image verification', () => {
  assert.equal(validateAuthChallenge({ challengeMode: 'image' }).challengeMode, 'image');
  assert.throws(() => validateAuthChallenge({}));
  assert.throws(() => validateAuthChallenge({ challengeMode: 'unknown' }));
});

test('Cloudflare configuration never falls back to an image challenge', () => {
  const config = { challengeMode: 'turnstile', turnstileSiteKey: 'public-key' };
  assert.deepEqual(validateAuthChallenge(config), config);
  assert.throws(() => validateAuthChallenge({ challengeMode: 'turnstile' }));
  assert.throws(() => validateAuthChallenge({ challengeMode: 'turnstile', turnstileSiteKey: ' ' }));
});

test('login starts without a challenge and gates image rendering and requests', () => {
  const source = readFileSync(new URL('../App.tsx', import.meta.url), 'utf8');
  assert.match(source, /useState<ApiAuthConfig>\(\{\}\)/);
  assert.match(source, /if \(authConfig.challengeMode !== 'image'\) return;/);
  assert.match(source, /authConfig.challengeMode === 'image' && <div className="hbdr-login-captcha-code"/);
  assert.doesNotMatch(source, /apiGet.*auth\/turnstile\/config/);
});
