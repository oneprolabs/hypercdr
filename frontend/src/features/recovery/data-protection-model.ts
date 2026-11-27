export type ConsistencyGroup = {
  id: string;
  strategy: 'offline-v1';
  applicationProfile: string;
  maxPauseSeconds: number;
  workloads: Array<{ namespace: string; kind: 'Deployment' | 'StatefulSet'; name: string; uid: string }>;
  volumes: Array<{ namespace: string; name: string; uid: string; snapshotClass: string }>;
};

export type DataProtectionSettings = {
  mode: 'live-filesystem' | 'snapshot-crash-consistent' | 'application-consistent';
  group?: ConsistencyGroup;
  riskAcknowledgement?: 'live-filesystem-no-application-consistency-v1';
};

export type SnapshotVolumeCheck = {
  namespace: string;
  name: string;
  uid: string;
  status: 'supported' | 'unsupported' | 'unknown' | 'error';
  reason: string;
};

export type SnapshotCapability = { type: string; name: string; driver?: string; fields?: Record<string, string> };

export function namespaceConsistencyWorkloads(capabilities: SnapshotCapability[], namespaces: string[], collectedAt: string | undefined, now: number) {
  return [...new Set(namespaces)].map(namespace => {
    const markers = capabilities.filter(item => item.type === 'ConsistencyNamespaceInventory' && item.name === namespace && item.fields?.namespace === namespace);
    const items = capabilities.filter(item => item.type === 'ConsistencyWorkload' && item.fields?.namespace === namespace);
    const marker = markers[0];
    const timestamp = marker?.fields?.collectedAt;
    const fresh = [Date.parse(timestamp || ''), Date.parse(collectedAt || '')].every(value => Number.isFinite(value) && now >= value && now - value <= 120_000);
    const complete = fresh && markers.length === 1 && marker.fields?.complete === 'true'
      && /^(0|[1-9]\d*)$/.test(marker.fields.workloadCount || '') && Number(marker.fields.workloadCount) === items.length
      && items.every(item => item.name && item.fields?.uid && item.fields.resourceVersion && item.fields.collectedAt === timestamp
        && ['Deployment', 'StatefulSet'].includes(item.fields.kind) && /^[1-9]\d*$/.test(item.fields.generation || '')
        && /^(0|[1-9]\d*)$/.test(item.fields.replicas || '') && ['true', 'false'].includes(item.fields.offlineOptIn)
        && ['true', 'false'].includes(item.fields.controllerManaged))
      && new Set(items.map(item => `${item.fields?.kind}/${item.name}`)).size === items.length
      && new Set(items.map(item => item.fields?.uid)).size === items.length;
    return { namespace, complete, workloads: complete ? items : [] };
  });
}

export function consistencyInventoryError(group: ConsistencyGroup | undefined, capabilities: SnapshotCapability[], namespaces: string[], collectedAt: string | undefined, now: number): string {
  if (!group) return 'Select an application coordination group.';
  const workloads = namespaceConsistencyWorkloads(capabilities, namespaces, collectedAt, now);
  const volumes = namespaceSnapshotPrerequisites(capabilities, namespaces, collectedAt, now);
  for (const workload of group.workloads) {
    const inventory = workloads.find(item => item.namespace === workload.namespace);
    const current = inventory?.workloads.find(item => item.name === workload.name && item.fields?.kind === workload.kind && item.fields.uid === workload.uid);
    if (!current) return 'Refresh resource inventory and reselect missing or replaced workloads before continuing.';
    if (current.fields?.offlineOptIn !== 'true' || current.fields?.controllerManaged !== 'false') return 'Every selected workload must explicitly allow offline backup and must not be controlled by an operator.';
  }
  for (const volume of group.volumes) {
    const inventory = volumes.find(item => item.namespace === volume.namespace);
    const current = inventory?.volumes.find(item => item.name === volume.name && item.fields?.uid === volume.uid);
    if (!current || current.fields?.status !== 'configured' || current.fields.snapshotClass !== volume.snapshotClass) return 'Refresh CSI inventory and reselect missing, replaced or unconfigured volumes before continuing.';
  }
  return '';
}

export function snapshotSetupGuidance(capability: SnapshotCapability): string {
  switch (capability.fields?.reasonCode) {
    case 'pvc-not-ready': return 'Wait for this PVC to be Bound and not deleting, then refresh the checks.';
    case 'pv-read-failed': return 'Ask the cluster administrator to check Agent access to the bound PersistentVolume and refresh the checks.';
    case 'pv-identity-mismatch': return 'Ask the cluster administrator to reconcile the PVC/PV binding. Do not reuse an old PVC identity.';
    case 'non-csi-volume': return 'Snapshot components alone cannot snapshot this volume. Migrate to a snapshot-capable CSI driver, or explicitly choose live file-level backup and acknowledge its consistency risk.';
    case 'snapshot-api-missing': return 'Ask the cluster administrator to install compatible snapshot CRDs, the snapshot controller and the storage driver’s CSI snapshotter, then configure a matching VolumeSnapshotClass. HyperCDR does not install these automatically.';
    case 'snapshot-class-read-failed': return 'Ask the cluster administrator to check snapshot API availability and Agent permissions to list VolumeSnapshotClasses. An access error does not prove snapshots are unsupported.';
    case 'snapshot-class-missing': return 'Create a VolumeSnapshotClass for the displayed CSI driver and check any velero.io/csi-volumesnapshot-class PVC override. Installing the controller alone is not sufficient.';
    case 'snapshot-class-ambiguous': return 'Ask the cluster administrator to select the intended driver-matching class using the velero.io/csi-volumesnapshot-class PVC annotation, then refresh the checks.';
    case 'prerequisites-configured': return 'Configuration matches only. Qualify snapshot creation, readiness and data movement on an isolated test volume before relying on it. Application consistency also requires a qualified coordination profile.';
    default: return 'Refresh the inventory from a connected Agent. If the result remains unverified, ask the cluster administrator to inspect the PVC, CSI driver and snapshot components; do not assume snapshot support.';
  }
}

export function namespaceSnapshotPrerequisites(capabilities: SnapshotCapability[], namespaces: string[], collectedAt: string | undefined, now: number) {
  return [...new Set(namespaces)].map(namespace => {
    const markers = capabilities.filter(item => item.type === 'CSISnapshotNamespaceInventory' && item.name === namespace && item.fields?.namespace === namespace);
    const volumes = capabilities.filter(item => item.type === 'CSISnapshotVolumePrerequisite' && item.fields?.namespace === namespace);
    const marker = markers[0];
    const timestamps = [Date.parse(marker?.fields?.collectedAt || ''), Date.parse(collectedAt || '')];
    const fresh = timestamps.every(collected => Number.isFinite(collected) && now - collected >= 0 && now - collected <= 120_000);
    const complete = fresh && markers.length === 1 && marker.fields?.complete === 'true'
      && /^(0|[1-9]\d*)$/.test(marker.fields.volumeCount || '') && Number(marker.fields.volumeCount) === volumes.length
      && volumes.every(item => item.name && item.fields?.uid)
      && new Set(volumes.map(item => item.name)).size === volumes.length
      && new Set(volumes.map(item => item.fields?.uid)).size === volumes.length;
    const statuses = volumes.map(item => item.fields?.status);
    const status = !complete ? 'unknown' : statuses.includes('error') ? 'error' : statuses.includes('unsupported') ? 'unsupported'
      : statuses.every(status => status === 'configured') ? 'configured' : 'unknown';
    return { namespace, status, volumes: complete ? volumes : [] };
  });
}

export function initialDataProtection(plan?: { dataProtection?: DataProtectionSettings }): DataProtectionSettings {
  if (!plan) return { mode: 'snapshot-crash-consistent' };
  return structuredClone(plan.dataProtection ?? { mode: 'live-filesystem' });
}

export type SnapshotPrerequisiteInventory = { capabilities: SnapshotCapability[]; collectedAt?: string; namespaceErrors?: Record<string, string> };

export function snapshotPrerequisiteResult(inventory: SnapshotPrerequisiteInventory, namespaces: string[], now: number) {
  const checks = namespaceSnapshotPrerequisites(inventory.capabilities, namespaces, inventory.collectedAt, now).map(check => {
    const error = inventory.namespaceErrors?.[check.namespace];
    return error ? { ...check, status: 'error', volumes: [], error } : { ...check, error: undefined };
  });
  const status: 'supported' | 'unsupported' | 'error' = !checks.length || checks.some(check => check.status === 'error' || check.status === 'unknown')
    ? 'error' : checks.some(check => check.status === 'unsupported') ? 'unsupported' : 'supported';
  return { status, checks };
}

export function snapshotPrerequisiteSummary(checks: ReturnType<typeof snapshotPrerequisiteResult>['checks']) {
  const summary = { namespaces: checks.length, volumes: 0, supported: 0, unsupported: 0, failed: 0, unverifiedNamespaces: 0 };
  for (const check of checks) {
    if (!check.volumes.length && check.status !== 'configured') summary.unverifiedNamespaces += 1;
    for (const volume of check.volumes) {
      summary.volumes += 1;
      if (volume.fields?.status === 'configured') summary.supported += 1;
      else if (volume.fields?.status === 'unsupported') summary.unsupported += 1;
      else summary.failed += 1;
    }
  }
  return summary;
}

export function fileLevelConsentValid(settings: DataProtectionSettings | undefined, accepted: boolean): boolean {
  return settings?.mode !== 'live-filesystem' || !settings.riskAcknowledgement || accepted;
}

export function consistencyGroupError(group: ConsistencyGroup | undefined, namespaces: string[]): string {
  if (!group?.id.trim() || group.strategy !== 'offline-v1' || !group.applicationProfile.trim() || !Number.isInteger(group.maxPauseSeconds) || group.maxPauseSeconds < 1 || group.maxPauseSeconds > 120 || !group.workloads.length || !group.volumes.length) return 'Specify a group, profile, workload, volume and pause limit of 1–120 seconds.';
  if (group.applicationProfile === 'paired-ledger-v1' && group.maxPauseSeconds <= 40) return 'This qualified profile reserves 40 seconds for application recovery. Choose a total pause limit of 41–120 seconds.';
  const identities = new Set<string>();
  const resources = new Set<string>();
  for (const item of [...group.workloads, ...group.volumes]) {
    const kind = 'kind' in item ? item.kind : 'PersistentVolumeClaim';
    const key = `${item.namespace}/${kind}/${item.name}`;
    if (!namespaces.includes(item.namespace) || !item.name.trim() || !item.uid.trim() || resources.has(key) || identities.has(item.uid)) return 'Every resource needs a unique current UID and must belong to a selected namespace.';
    if ('kind' in item && item.kind !== 'Deployment' && item.kind !== 'StatefulSet') return 'Only Deployment and StatefulSet workloads are supported.';
    if ('snapshotClass' in item && !item.snapshotClass.trim()) return 'Select a snapshot class for every volume.';
    resources.add(key);
    identities.add(item.uid);
  }
  return '';
}

export function snapshotReadiness(
  selectedVolumes: Array<{ namespace: string; name: string; uid: string }>,
  checks: SnapshotVolumeCheck[],
  inventoryComplete: boolean,
): SnapshotVolumeCheck['status'] {
  if (!inventoryComplete) return 'unknown';
  const results = selectedVolumes.map(volume => {
    const matches = checks.filter(check => check.namespace === volume.namespace && check.name === volume.name && check.uid === volume.uid);
    return volume.uid && matches.length === 1 ? matches[0].status : 'unknown';
  });
  if (results.includes('error')) return 'error';
  if (results.includes('unsupported')) return 'unsupported';
  if (results.includes('unknown')) return 'unknown';
  return 'supported';
}

export const snapshotPauseExplanation = 'Application stop/freeze coordination is no longer supported. Explicitly select CSI snapshots for driver-provided crash consistency without pausing application writes.';

export function backupExecutionExplanation(mode: DataProtectionSettings['mode'] | undefined): string {
  if (mode === 'application-consistent') return snapshotPauseExplanation;
  if (mode === 'snapshot-crash-consistent') return 'This mode does not coordinate or pause application writes. Data transfers from stable CSI snapshots, but snapshots across volumes may be taken at different times. Application consistency is not guaranteed.';
  return 'This mode reads live volume files while applications continue writing. It does not create a stable CSI snapshot or guarantee application consistency.';
}
