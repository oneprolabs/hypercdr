import assert from 'node:assert/strict';
import test from 'node:test';
import { workloadReadinessSnapshot, workloadReadinessEvents } from './workload-readiness.ts';

test('old agents have no structured snapshot', () => {
  assert.equal(workloadReadinessSnapshot({ payload: {} }, []), null);
});

test('summary prioritizes pod blocker and keeps workload counts distinct', () => {
  const snapshot = workloadReadinessSnapshot({ payload: { velero: { readiness: {
    ReadyWorkloads: 1, WorkloadCount: 2, ReadyPodCount: 3, PodCount: 4,
    Objects: [{ Kind: 'deployment', Name: 'nextcloud', State: 'Waiting for replicas' }, { Kind: 'Pod', Name: 'nextcloud-123', State: 'Starting', Reason: 'ImagePullBackOff' }],
  } } } }, []);
  assert.equal(snapshot.summary, '1/2 workloads ready · nextcloud-123: ImagePullBackOff');
});

test('observation failures do not claim namespace is missing', () => {
  const snapshot = workloadReadinessSnapshot({ payload: { velero: { readinessObservationError: 'Unable to read Kubernetes readiness; retrying.' } } }, []);
  assert.equal(snapshot.summary, 'Unable to read Kubernetes readiness; retrying.');
});

test('event snapshot supports historical tasks', () => {
  assert.match(workloadReadinessSnapshot({ payload: {} }, [{ payload: { velero: { readiness: { ReadyWorkloads: 2, WorkloadCount: 2, Message: 'Ready' } } } }]).summary, /2\/2 workloads ready/);
});

test('active task with stale observations does not imply live readiness', () => {
  assert.match(workloadReadinessSnapshot({ status: 'running', payload: { readiness: { Ready: false }, readinessObservedAt: '2026-01-01T00:00:00Z' } }, []).summary, /stale/);
});

test('persisted snapshot takes precedence over old event data', () => {
  const snapshot = workloadReadinessSnapshot({ payload: { readiness: { ReadyWorkloads: 2, WorkloadCount: 2, Ready: true } } }, [{ payload: { velero: { readiness: { ReadyWorkloads: 0, WorkloadCount: 2 } } } }]);
  assert.match(snapshot.summary, /2\/2 workloads ready/);
});

test('timeline ends with explicit readiness completion and does not invent pull events', () => {
  const task = { status: 'succeeded', payload: { readinessStartedAt: '2026-10-23T09:00:00Z', readinessObservedAt: '2026-10-23T09:00:15Z', readinessStage: 'succeeded', readiness: { Ready: true, Objects: [{ Kind: 'Pod', UID: 'pod-1', Name: 'nextcloud-123', DisplayName: 'nextcloud', State: 'Ready' }] } } };
  const rows = workloadReadinessEvents(task, []);
  assert.equal(rows.at(-1).summary, 'All workloads are ready. Completed in 15s.');
  assert.equal(rows.filter(row => row.image).length, 0);
  assert.ok(rows.some(row => row.summary === 'nextcloud: Ready.'));
});

test('multiple containers retain full image addresses and aggregate image retries', () => {
  const image = { ID: 'pull-1', PodUID: 'pod-1', Pod: 'nextcloud-123', DisplayName: 'nextcloud', Container: 'app', Image: 'mapped.registry/apps/nextcloud@sha256:1234', Reason: 'Pulling', FirstAt: '2026-10-23T09:00:01Z' };
  const sample = payload => ({ createdAt: '2026-10-23T09:00:10Z', payload: { velero: { readinessStage: 'started', readiness: { ImageEvents: payload } } } });
  const failed = { ...image, ID: 'failed-1', Reason: 'Failed', Count: 2, Message: 'Failed to pull image: i/o timeout' };
  const rows = workloadReadinessEvents({ status: 'running', payload: {} }, [sample([image, failed, { ...image, ID: 'sidecar-1', Container: 'sidecar', Image: 'registry/sidecar:v1' }]), sample([{ ...failed, Count: 6 }])]);
  assert.equal(rows.filter(row => row.summary.includes('Image pull failed')).length, 1);
  assert.equal(rows.find(row => row.summary.includes('Image pull failed')).count, 6);
  assert.match(rows.find(row => row.summary.includes('Image pull failed')).summary, /timed out/);
  assert.ok(rows.some(row => row.image === image.Image));
  assert.ok(rows.some(row => row.image === 'registry/sidecar:v1'));
});

test('cached images are distinct from successful downloads and pods stay separate', () => {
  const image = { ID: 'event-1', PodUID: 'pod-1', Pod: 'app-1', Container: 'app', Image: 'registry/app:v1', Reason: 'ImageCached', FirstAt: '2026-10-23T09:00:01Z' };
  const rows = workloadReadinessEvents({ payload: { readiness: { ImageEvents: [image, { ...image, ID: 'event-2', PodUID: 'pod-2', Pod: 'app-2' }] } } }, []);
  assert.equal(rows.filter(row => row.summary.includes('locally cached')).length, 2);
  assert.equal(rows.filter(row => row.summary.includes('pulled successfully')).length, 0);
});
