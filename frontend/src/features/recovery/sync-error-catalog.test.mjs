import assert from 'node:assert/strict';
import test from 'node:test';
import { SYNC_ERROR_CATALOG } from './sync-error-catalog.ts';

test('sync failure classifications have unique numeric codes and actionable details', () => {
  const aliases = SYNC_ERROR_CATALOG.flatMap(item => item.aliases);
  assert.equal(new Set(SYNC_ERROR_CATALOG.map(item => item.code)).size, SYNC_ERROR_CATALOG.length);
  assert.equal(new Set(aliases).size, aliases.length);
  for (const item of SYNC_ERROR_CATALOG) {
    assert.match(item.code, /^\d{6}$/);
    assert.ok(item.title && item.description && item.detail.length > 60);
  }
  for (const suffix of ['STORAGE_AUTH_FAILED', 'STORAGE_CAPACITY_EXCEEDED', 'TLS_FAILED', 'API_PERMISSION_DENIED', 'REPOSITORY_UNAVAILABLE', 'CSI_RUNTIME_UNAVAILABLE', 'CSI_PREREQUISITES_FAILED', 'CSI_SNAPSHOT_FAILED', 'VOLUME_MOUNT_FAILED', 'WORKER_RESOURCE_UNAVAILABLE', 'SOURCE_VOLUME_UNAVAILABLE', 'NETWORK_UNAVAILABLE', 'DEADLINE_EXCEEDED', 'API_READ_FAILED', 'BACKUP_FAILED']) {
    assert.ok(aliases.includes(`SYNC_${suffix}`), suffix);
  }
  assert.ok(aliases.includes('SNAPSHOT_COMPLETION_UNPROVEN'));
});
