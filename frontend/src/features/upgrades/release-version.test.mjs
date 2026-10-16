import test from 'node:test';
import assert from 'node:assert/strict';
import { compareReleaseVersions } from './release-version.ts';

test('new numbering follows legacy releases and preserves numeric order', () => {
  assert.ok(compareReleaseVersions('ce-v0.0.0', '1.0.142.20261015') > 0);
  assert.ok(compareReleaseVersions('ee-v0.0.0', 'v1.0.142.20261015-ent.2') > 0);
  assert.ok(compareReleaseVersions('ce-v0.0.10', 'ce-v0.0.2') > 0);
  assert.ok(compareReleaseVersions('1.0.142.20261015', 'ce-v0.0.0') < 0);
  assert.equal(compareReleaseVersions('ce-v0.0.0', 'ee-v0.0.1'), 0);
  assert.equal(compareReleaseVersions('dev', 'ce-v0.0.0'), 0);
});
