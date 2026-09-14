const { test } = require('node:test');
const assert = require('node:assert/strict');
const { load, entriesFor } = require('../site/assets/catalog.js');
const community = { version: '1.0.22.20260914' };
const enterprise = { version: 'v20260914.1-ent.1' };
test('catalog isolates editions and sorts versions numerically', () => {
  assert.deepEqual(entriesFor({ items: [enterprise, community] }, 'community'), [community]);
  assert.deepEqual(entriesFor({ items: [community, enterprise] }, 'enterprise'), [enterprise]);
});
for (const failure of ['network', 'status', 'json', 'empty']) {
  test(`falls back to local index after ${failure} failure`, async () => {
    const calls = [];
    const result = await load('community', './releases/community', async url => {
      calls.push(url);
      if (calls.length === 1) {
        if (failure === 'network') throw new Error('connection refused');
        return { ok: failure !== 'status', json: async () => {
          if (failure === 'json') throw new Error('invalid JSON');
          return { items: [] };
        } };
      }
      return { ok: true, json: async () => ({ items: [community] }) };
    });
    assert.equal(result.source, 'local');
    assert.deepEqual(result.entries, [community]);
    assert.equal(calls[1], './releases/community/index.json');
  });
}
test('prefers remote catalog without requesting local index', async () => {
  let calls = 0;
  const result = await load('community', './releases/community', async () => {
    calls++;
    return { ok: true, json: async () => ({ items: [community] }) };
  });
  assert.equal(calls, 1);
  assert.equal(result.source, 'release-center');
});
