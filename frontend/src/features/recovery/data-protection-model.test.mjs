import test from 'node:test';
import assert from 'node:assert/strict';
import { backupExecutionExplanation, consistencyGroupError, consistencyInventoryError, namespaceConsistencyWorkloads, fileLevelConsentValid, initialDataProtection, snapshotReadiness, namespaceSnapshotPrerequisites, snapshotSetupGuidance, snapshotPrerequisiteResult, snapshotPrerequisiteSummary } from './data-protection-model.ts';

test('multi-namespace CSI summary keeps same-name PVCs separate and preserves partial failures', () => {
  const now = Date.parse('2026-11-26T10:00:00Z');
  const collectedAt = new Date(now).toISOString();
  const capabilities = ['alpha', 'beta'].flatMap(namespace => [
    { type: 'CSISnapshotNamespaceInventory', name: namespace, fields: { namespace, collectedAt, complete: 'true', volumeCount: '2' } },
    { type: 'CSISnapshotVolumePrerequisite', name: 'data', fields: { namespace, uid: namespace + '-data', status: 'configured', storageClass: 'longhorn' } },
    { type: 'CSISnapshotVolumePrerequisite', name: 'logs', fields: { namespace, uid: namespace + '-logs', status: namespace === 'alpha' ? 'unsupported' : 'error' } },
  ]);
  const result = snapshotPrerequisiteResult({ capabilities, collectedAt }, ['alpha', 'beta', 'alpha'], now);
  assert.deepEqual(snapshotPrerequisiteSummary(result.checks), { namespaces: 2, volumes: 4, supported: 2, unsupported: 1, failed: 1, unverifiedNamespaces: 0 });
  assert.deepEqual(result.checks.map(check => check.volumes[0].name), ['data', 'data']);
  const partial = snapshotPrerequisiteResult({ capabilities, collectedAt, namespaceErrors: { beta: 'Agent request timed out' } }, ['alpha', 'beta'], now);
  assert.equal(partial.status, 'error');
  assert.equal(partial.checks[1].error, 'Agent request timed out');
  assert.deepEqual(snapshotPrerequisiteSummary(partial.checks), { namespaces: 2, volumes: 2, supported: 1, unsupported: 1, failed: 0, unverifiedNamespaces: 1 });
  assert.equal(snapshotPrerequisiteSummary(snapshotPrerequisiteResult({ capabilities: [] }, ['alpha', 'beta'], now).checks).unverifiedNamespaces, 2);
});

test('one-shot CSI checks distinguish supported, unsupported and failed detection', () => {
  const now = Date.parse('2026-11-26T10:00:00Z');
  const collectedAt = new Date(now).toISOString();
  const inventory = { collectedAt, capabilities: [
    { type: 'CSISnapshotNamespaceInventory', name: 'app', fields: { namespace: 'app', collectedAt, complete: 'true', volumeCount: '1' } },
    { type: 'CSISnapshotVolumePrerequisite', name: 'data', fields: { namespace: 'app', uid: 'pvc-uid', status: 'configured' } },
  ] };
  const completed = snapshotPrerequisiteResult(inventory, ['app'], now);
  assert.equal(completed.status, 'supported');
  for (const status of ['unsupported', 'error', 'unknown']) {
    const changed = structuredClone(inventory);
    changed.capabilities[1].fields.status = status;
    assert.equal(snapshotPrerequisiteResult(changed, ['app'], now).status, status === 'unsupported' ? 'unsupported' : 'error');
  }
  assert.equal(snapshotPrerequisiteResult(inventory, ['app', 'missing'], now).status, 'error');
  assert.equal(snapshotPrerequisiteResult(inventory, [], now).status, 'error');
  assert.equal(snapshotPrerequisiteResult({ capabilities: [] }, ['app'], now).status, 'error');
  assert.equal(snapshotPrerequisiteResult(inventory, ['app'], now + 120_001).status, 'error');
  inventory.capabilities.length = 0;
  assert.equal(completed.status, 'supported');
  assert.equal(completed.checks[0].volumes.length, 1);
});

test('workload choices require fresh, complete and exact namespace resource inventory', () => {
  const now = Date.parse('2026-11-25T10:00:00Z');
  const collectedAt = new Date(now - 1000).toISOString();
  const marker = { type: 'ConsistencyNamespaceInventory', name: 'app', fields: { namespace: 'app', collectedAt, complete: 'true', workloadCount: '1' } };
  const workload = { type: 'ConsistencyWorkload', name: 'writer', fields: { namespace: 'app', uid: 'writer-uid', kind: 'Deployment', resourceVersion: '12', generation: '3', replicas: '1', offlineOptIn: 'true', controllerManaged: 'false', collectedAt } };
  const result = items => namespaceConsistencyWorkloads(items, ['app'], collectedAt, now)[0];
  assert.equal(result([marker, workload]).complete, true);
  assert.equal(result([marker, workload]).workloads[0].fields.uid, 'writer-uid');
  for (const items of [[], [marker], [marker, marker, workload], [marker, workload, workload], [marker, { ...workload, fields: { ...workload.fields, uid: '' } }], [marker, { ...workload, fields: { ...workload.fields, collectedAt: '2026-11-24T10:00:00Z' } }], [marker, { ...workload, fields: { ...workload.fields, kind: 'Pod' } }]]) {
    assert.deepEqual(result(items).workloads, []);
    assert.equal(result(items).complete, false);
  }
  assert.equal(namespaceConsistencyWorkloads([marker, workload], ['app'], collectedAt, now + 120_000)[0].complete, false);
  assert.equal(namespaceConsistencyWorkloads([marker, workload], ['app'], undefined, now)[0].complete, false);
  assert.equal(namespaceConsistencyWorkloads([marker, workload], ['other'], collectedAt, now)[0].complete, false);
  const statefulSet = { ...workload, fields: { ...workload.fields, kind: 'StatefulSet', uid: 'stateful-uid' } };
  assert.equal(result([{ ...marker, fields: { ...marker.fields, workloadCount: '2' } }, workload, statefulSet]).complete, true);
});

test('group inventory rejects replaced identities and never updates a saved group implicitly', () => {
  const now = Date.parse('2026-11-25T10:00:00Z');
  const collectedAt = new Date(now - 1000).toISOString();
  const group = { workloads: [{ namespace: 'app', name: 'writer', kind: 'Deployment', uid: 'writer-uid' }], volumes: [{ namespace: 'app', name: 'data', uid: 'pvc-uid', snapshotClass: 'csi-class' }] };
  const capabilities = [
    { type: 'ConsistencyNamespaceInventory', name: 'app', fields: { namespace: 'app', collectedAt, complete: 'true', workloadCount: '1' } },
    { type: 'ConsistencyWorkload', name: 'writer', fields: { namespace: 'app', uid: 'writer-uid', kind: 'Deployment', resourceVersion: '12', generation: '3', replicas: '1', offlineOptIn: 'true', controllerManaged: 'false', collectedAt } },
    { type: 'CSISnapshotNamespaceInventory', name: 'app', fields: { namespace: 'app', collectedAt, complete: 'true', volumeCount: '1' } },
    { type: 'CSISnapshotVolumePrerequisite', name: 'data', fields: { namespace: 'app', collectedAt, uid: 'pvc-uid', status: 'configured', snapshotClass: 'csi-class' } },
  ];
  const validate = items => consistencyInventoryError(group, items, ['app'], collectedAt, now);
  assert.equal(validate(capabilities), '');
  const original = structuredClone(group);
  for (const [index, patch] of [[1, { uid: 'replacement' }], [1, { offlineOptIn: 'false' }], [1, { controllerManaged: 'true' }], [3, { uid: 'replacement' }], [3, { status: 'unsupported' }], [3, { snapshotClass: 'changed-class' }]]) {
    const changed = structuredClone(capabilities);
    Object.assign(changed[index].fields, patch);
    assert.notEqual(validate(changed), '');
    assert.deepEqual(group, original);
  }
  assert.match(consistencyInventoryError(group, capabilities, ['app'], collectedAt, now + 120_000), /Refresh/);
});

test('snapshot setup guidance distinguishes missing infrastructure, access failures and unverified configuration', () => {
  const guidance = reasonCode => snapshotSetupGuidance({ type: 'CSISnapshotVolumePrerequisite', name: 'data', fields: { reasonCode } });
  assert.match(guidance('non-csi-volume'), /components alone cannot snapshot/);
  assert.match(guidance('non-csi-volume'), /explicitly choose/);
  assert.match(guidance('snapshot-api-missing'), /does not install these automatically/);
  assert.match(guidance('snapshot-class-missing'), /controller alone is not sufficient/);
  assert.match(guidance('snapshot-class-ambiguous'), /velero.io\/csi-volumesnapshot-class/);
  assert.match(guidance('snapshot-class-read-failed'), /does not prove snapshots are unsupported/);
  assert.match(guidance('prerequisites-configured'), /Configuration matches only/);
  assert.match(guidance('prerequisites-configured'), /isolated test volume/);
  assert.match(guidance('pv-identity-mismatch'), /Do not reuse/);
  assert.match(guidance('pv-read-failed'), /Agent access/);
  assert.match(guidance('pvc-not-ready'), /Bound/);
  assert.match(guidance(undefined), /do not assume snapshot support/);
  assert.equal(guidance('future-code'), guidance(undefined));
});

test('execution explanations reject legacy coordination and distinguish snapshots from live files', () => {
  assert.match(backupExecutionExplanation('application-consistent'), /no longer supported/);
  assert.match(backupExecutionExplanation('snapshot-crash-consistent'), /does not coordinate or pause/);
  assert.match(backupExecutionExplanation('snapshot-crash-consistent'), /different times/);
  assert.match(backupExecutionExplanation('live-filesystem'), /reads live volume files/);
  assert.equal(backupExecutionExplanation(undefined), backupExecutionExplanation('live-filesystem'));
});

test('explicit coordination groups require complete unique in-scope identities', () => {
  const valid = { id: 'group', strategy: 'offline-v1', applicationProfile: 'qualified', maxPauseSeconds: 120,
    workloads: [{ namespace: 'test', kind: 'Deployment', name: 'writer', uid: 'writer-uid' }],
    volumes: [{ namespace: 'test', name: 'data', uid: 'data-uid', snapshotClass: 'class' }] };
  assert.equal(consistencyGroupError(valid, ['test']), '');
  for (const maxPauseSeconds of [1, 39, 40]) {
    assert.match(consistencyGroupError({ ...valid, applicationProfile: 'paired-ledger-v1', maxPauseSeconds }, ['test']), /reserves 40 seconds/);
  }
  for (const maxPauseSeconds of [41, 120]) {
    assert.equal(consistencyGroupError({ ...valid, applicationProfile: 'paired-ledger-v1', maxPauseSeconds }, ['test']), '');
  }
  assert.equal(consistencyGroupError({ ...valid, maxPauseSeconds: 1 }, ['test']), '');
  for (const change of [
    group => { group.id = ''; }, group => { group.applicationProfile = ''; },
    group => { group.maxPauseSeconds = 121; }, group => { group.maxPauseSeconds = 0; },
    group => { group.maxPauseSeconds = 1.5; }, group => { group.workloads = []; },
    group => { group.volumes = []; }, group => { group.volumes[0].namespace = 'other'; },
    group => { group.volumes[0].uid = 'writer-uid'; }, group => { group.volumes[0].snapshotClass = ''; },
    group => { group.workloads[0].kind = 'Pod'; }, group => { group.workloads[0].uid = ''; },
    group => { group.volumes.push({ ...group.volumes[0], uid: 'different-uid' }); },
  ]) {
    const candidate = structuredClone(valid);
    change(candidate);
    assert.notEqual(consistencyGroupError(candidate, ['test']), '');
  }
  assert.notEqual(consistencyGroupError(undefined, ['test']), '');
  assert.notEqual(consistencyGroupError(valid, []), '');
});

test('revoking file-level consent blocks progress without changing backup mode', () => {
  const settings = { mode: 'live-filesystem', riskAcknowledgement: 'live-filesystem-no-application-consistency-v1' };
  assert.equal(fileLevelConsentValid(settings, true), true);
  assert.equal(fileLevelConsentValid(settings, false), false);
  assert.equal(settings.mode, 'live-filesystem');
  assert.equal(fileLevelConsentValid({ mode: 'snapshot-crash-consistent' }, false), true);
  assert.equal(fileLevelConsentValid({ mode: 'application-consistent' }, false), true);
  assert.equal(fileLevelConsentValid(initialDataProtection({}), false), true);
});

const volume = { namespace: 'test', name: 'data', uid: 'current-pvc' };
const supported = { ...volume, status: 'supported', reason: 'Verified' };

test('namespace prerequisite results require fresh complete exact inventory', () => {
  const now = Date.parse('2026-11-25T00:00:00Z');
  const collectedAt = new Date(now).toISOString();
  const marker = { type: 'CSISnapshotNamespaceInventory', name: 'test', fields: { namespace: 'test', complete: 'true', volumeCount: '1', collectedAt } };
  const check = { type: 'CSISnapshotVolumePrerequisite', name: 'data', fields: { namespace: 'test', uid: 'current-pvc', status: 'configured' } };
  const status = (items, timestamp = collectedAt) => namespaceSnapshotPrerequisites(items, ['test'], timestamp, now)[0].status;
  assert.equal(status([marker, check]), 'configured');
  assert.equal(status([check]), 'unknown');
  assert.equal(status([marker]), 'unknown');
  assert.equal(status([marker, marker, check]), 'unknown');
  assert.equal(status([marker, check], new Date(now - 120_001).toISOString()), 'unknown');
  assert.equal(status([marker, check], new Date(now + 1).toISOString()), 'unknown');
  assert.equal(status([marker, check], 'invalid'), 'unknown');
  assert.equal(status([{ ...marker, fields: { ...marker.fields, collectedAt: new Date(now - 120_001).toISOString() } }, check]), 'unknown');
  assert.equal(status([marker, { ...check, fields: { ...check.fields, status: 'unsupported' } }]), 'unsupported');
  assert.equal(status([marker, { ...check, fields: { ...check.fields, status: 'error' } }]), 'error');
  assert.equal(namespaceSnapshotPrerequisites([marker, check], ['other'], collectedAt, now)[0].status, 'unknown');
});

test('new plans default to CSI, legacy plans stay file-level', () => {
  assert.equal(initialDataProtection().mode, 'snapshot-crash-consistent');
  assert.equal(initialDataProtection({}).mode, 'live-filesystem');
});

test('risk acknowledgement is preserved only when already recorded', () => {
  const riskAcknowledgement = 'live-filesystem-no-application-consistency-v1';
  assert.deepEqual(initialDataProtection({ dataProtection: { mode: 'live-filesystem', riskAcknowledgement } }), { mode: 'live-filesystem', riskAcknowledgement });
  assert.equal(initialDataProtection({}).riskAcknowledgement, undefined);
  assert.equal(initialDataProtection().riskAcknowledgement, undefined);
});

test('editing preserves independent copies of explicit consistency groups', () => {
  const plan = { dataProtection: { mode: 'application-consistent', group: { volumes: [volume] } } };
  const settings = initialDataProtection(plan);
  settings.group.volumes[0].name = 'changed';
  assert.equal(plan.dataProtection.group.volumes[0].name, 'data');
});

test('every selected PVC must have exactly one current identity check', () => {
  assert.equal(snapshotReadiness([volume], [supported], true), 'supported');
  assert.equal(snapshotReadiness([volume], [], true), 'unknown');
  assert.equal(snapshotReadiness([volume], [{ ...supported, uid: 'old-pvc' }], true), 'unknown');
  assert.equal(snapshotReadiness([volume], [supported, supported], true), 'unknown');
  assert.equal(snapshotReadiness([volume], [supported], false), 'unknown');
});

test('unsupported and failed probes remain distinguishable', () => {
  assert.equal(snapshotReadiness([volume], [{ ...supported, status: 'unsupported' }], true), 'unsupported');
  assert.equal(snapshotReadiness([volume], [{ ...supported, status: 'error' }], true), 'error');
  assert.equal(snapshotReadiness([], [], true), 'supported');
  assert.equal(snapshotReadiness([], [], false), 'unknown');
});
