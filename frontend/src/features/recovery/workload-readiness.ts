import type { ApiTask, ApiTaskEvent } from './types.ts';
import { isActiveTaskStatus } from './task-status.ts';

export function workloadReadinessSnapshot(task: ApiTask, events: ApiTaskEvent[]) {
  const candidates = [{ payload: task.payload }, { payload: task.payload?.velero }, ...[...events].reverse().map(event => ({ payload: event.payload?.velero, observedAt: event.createdAt }))];
  const candidate = candidates.find(value => value.payload && (value.payload.readiness || value.payload.readinessObservationError));
  if (!candidate) return null;
  const { payload } = candidate;
  const readiness = payload.readiness || {};
  const objects = Array.isArray(readiness.Objects) ? readiness.Objects : [];
  const waiting = objects.filter(object => !['Ready', 'Bound'].includes(object.State));
  const pod = waiting.find(object => object.Kind === 'Pod');
  const blocker = pod || waiting[0];
  const error = String(payload.readinessObservationError || '');
  const observedAt = payload.readinessObservedAt || ('observedAt' in candidate ? candidate.observedAt : undefined);
  const stale = isActiveTaskStatus(task.status) && !readiness.Ready && Date.now() - Date.parse(observedAt || '') > 90_000;
  const summary = error || (stale ? 'Readiness observation is stale; showing last known state.' : `${readiness.ReadyWorkloads || 0}/${readiness.WorkloadCount || 0} workloads ready · ${blocker ? `${blocker.Name}: ${blocker.Reason || blocker.EventReason || blocker.State}` : readiness.Message || 'Checking readiness'}`);
  return { readiness, objects, summary, error, startedAt: payload.readinessStartedAt, observedAt, deadline: payload.readinessDeadline };
}

export type WorkloadTimelineEvent = {
  id: string;
  at: string;
  summary: string;
  status: 'completed' | 'waiting' | 'warning' | 'failed';
  image?: string;
  imageInDetails?: boolean;
  detail?: string;
  count?: number;
  lastAt?: string;
};

export function workloadReadinessEvents(task: ApiTask, events: ApiTaskEvent[]): WorkloadTimelineEvent[] {
  const samples = events.map(event => ({ payload: event.payload?.velero || event.payload || {}, at: event.createdAt }));
  samples.push({ payload: task.payload?.velero || {}, at: task.completedAt || task.createdAt });
  samples.push({ payload: task.payload || {}, at: task.completedAt || task.createdAt });
  samples.sort((left, right) => Date.parse(left.payload.readinessObservedAt || left.at || '') - Date.parse(right.payload.readinessObservedAt || right.at || ''));
  const rows = new Map<string, WorkloadTimelineEvent>();
  const states = new Map<string, string>();
  let lastError = '';
  let latestPayload: Record<string, any> | undefined;
  for (const sample of samples) {
    const payload = sample.payload;
    if (!payload.readiness && !payload.readinessStage && !payload.readinessObservationError) continue;
    latestPayload = payload;
    const at = String(payload.readinessObservedAt || sample.at || '');
    if (payload.readinessStartedAt && !rows.has('started')) rows.set('started', { id: 'started', at: payload.readinessStartedAt, summary: 'Checking restored workload readiness.', status: 'waiting' });
    const error = String(payload.readinessObservationError || '');
    if (error && error !== lastError) rows.set(`observation-${at}`, { id: `observation-${at}`, at, summary: error, status: 'warning' });
    if (!error && lastError) rows.set(`observation-recovered-${at}`, { id: `observation-recovered-${at}`, at, summary: 'Workload status checks resumed.', status: 'completed' });
    lastError = error;
    for (const image of payload.readiness?.ImageEvents || []) {
      const key = `${image.PodUID}/${image.Container}/${image.Image}`;
      const failure = ['Failed', 'BackOff'].includes(image.Reason);
      const id = failure ? `image-failure-${key}` : `image-${image.ID || `${key}/${image.Reason}`}`;
      const subject = `${image.DisplayName || image.Pod}${image.Container ? ` / ${image.Container}` : ''}`;
      const message = String(image.Message || '').toLowerCase();
      const cause = /timeout|timed out/.test(message) ? 'registry connection timed out' : /unauthorized|authentication required|pull access denied/.test(message) ? 'registry access denied' : /manifest unknown|not found/.test(message) ? 'image not found' : /certificate|tls/.test(message) ? 'registry TLS error' : '';
      const action = image.Reason === 'Pulling' ? 'Pulling image' : image.Reason === 'ImageCached' ? 'Using locally cached image' : image.Reason === 'Pulled' ? 'Image pulled successfully' : `Image pull failed${cause ? `: ${cause}` : ''}`;
      const previous = rows.get(id);
      if (failure && previous && image.Reason === 'BackOff') { previous.count = Math.max(previous.count || 1, image.Count || 1); if (Date.parse(image.LastAt || '') > Date.parse(previous.lastAt || '')) previous.lastAt = image.LastAt; continue; }
      rows.set(id, { id, at: previous?.at || image.FirstAt || at, summary: `${subject}: ${action}.`, status: failure ? 'warning' : image.Reason === 'Pulling' ? 'waiting' : 'completed', image: image.Image, imageInDetails: image.Reason === 'Pulled', detail: `${image.Pod}${image.Node ? ` · Node ${image.Node}` : ''}\n${image.Message || ''}`, count: Math.max(previous?.count || 1, image.Count || 1), lastAt: image.LastAt });
    }
    const pods = (payload.readiness?.Objects || []).filter(object => object.Kind === 'Pod');
    for (const object of pods) {
      const key = object.UID || `${object.Kind}/${object.Name}`;
      const state = `${object.State}/${object.Reason || ''}/${object.Container || ''}`;
      if (states.get(key) === state) continue;
      const previous = states.get(key);
      states.set(key, state);
      if (/imagepull|errimage|backoff/i.test(object.Reason || '') && (payload.readiness?.ImageEvents || []).some(image => image.PodUID === object.UID && ['Failed', 'BackOff'].includes(image.Reason))) continue;
      const matchingWorkload = (payload.readiness?.Objects || []).filter(candidate => ['deployment', 'statefulset', 'daemonset'].includes(candidate.Kind) && object.Name.startsWith(`${candidate.Name}-`)).sort((left, right) => right.Name.length - left.Name.length)[0];
      const displayName = object.DisplayName || matchingWorkload?.Name || object.Name;
      const subject = `${displayName}${object.Container && object.Container !== displayName ? ` / ${object.Container}` : ''}`;
      const ready = object.State === 'Ready';
      const action = /imagepull|errimage|backoff/i.test(object.Reason || '') ? 'Image pull failed' : ready ? 'Ready' : object.State === 'Scheduling' ? 'Waiting for scheduling' : object.State === 'Initializing' ? 'Initializing' : object.State === 'Waiting for readiness' ? 'Started; waiting for readiness checks' : 'Starting';
      const warning = /failed|error|backoff|unschedulable|unhealthy/i.test(object.Reason || '') || (previous?.startsWith('Ready/') && !ready);
      const id = `object-${key}-${at}`;
      rows.set(id, { id, at: object.Since || at, summary: `${subject}: ${action}.`, status: ready ? 'completed' : warning ? 'warning' : 'waiting', detail: `${object.Name}${object.Node ? ` · Node ${object.Node}` : ''}\n${object.Reason || ''}${object.Message ? `: ${object.Message}` : ''}`, image: /imagepull|errimage|backoff/i.test(object.Reason || '') ? object.Images?.[object.Container] : undefined });
    }
    if (!pods.length && payload.readiness && !payload.readiness.Ready) {
      const readiness = payload.readiness;
      const key = `counts-${readiness.ReadyWorkloads || 0}-${readiness.WorkloadCount || 0}`;
      if (!rows.has(key)) rows.set(key, { id: key, at, summary: `${readiness.ReadyWorkloads || 0}/${readiness.WorkloadCount || 0} workloads ready; waiting for the remaining workloads.`, status: 'waiting' });
    }
  }
  if (latestPayload?.readinessStage === 'succeeded' || latestPayload?.readiness?.Ready) {
    const at = String(latestPayload.readinessObservedAt || task.completedAt || '');
    const elapsed = Math.max(0, Math.floor((Date.parse(at) - Date.parse(latestPayload.readinessStartedAt || '')) / 1000));
    rows.set('completed', { id: 'completed', at, summary: `All workloads are ready.${Number.isFinite(elapsed) ? ` Completed in ${elapsed}s.` : ''}`, status: 'completed' });
  } else if (latestPayload && isActiveTaskStatus(task.status)) {
    const observedAt = Date.parse(latestPayload.readinessObservedAt || '');
    if (Date.now() - observedAt > 90_000 && !lastError) rows.set('stale', { id: 'stale', at: latestPayload.readinessObservedAt, summary: 'Workload status updates are delayed; showing the last known state.', status: 'warning' });
  }
  return [...rows.values()].sort((left, right) => (Date.parse(left.at) || 0) - (Date.parse(right.at) || 0) || (left.id === 'completed' ? 1 : right.id === 'completed' ? -1 : left.id === 'started' ? -1 : right.id === 'started' ? 1 : left.id.localeCompare(right.id)));
}
