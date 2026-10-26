export function restorePointRetentionLabels(metadata?: Record<string, unknown>) {
  const labels: string[] = [];
  if (metadata?.manualRetained === true) labels.push('Manual retention');
  if (metadata?.drillRetained === true) labels.push('Drill retention');
  if (Array.isArray(metadata?.retentionReasons)) {
    for (const raw of metadata.retentionReasons) {
      if (!raw || typeof raw !== 'object') continue;
      const reason = raw as Record<string, unknown>;
      const period = { daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly', yearly: 'Yearly' }[String(reason.period)];
      if (period && !labels.includes(`${period} retention`)) labels.push(`${period} retention`);
    }
  }
  if (metadata?.replacementTaskId) labels.push('Pending replacement');
  if (metadata?.inUse === true) labels.push('In use');
  if (metadata?.retentionState === 'deleting' || metadata?.retentionState === 'pending_delete') labels.push('Deleting');
  return labels;
}

export function restorePointMutationLocked(metadata?: Record<string, unknown>) {
  return Boolean(metadata?.inUse === true || metadata?.replacementTaskId || metadata?.retentionState === 'deleting' || metadata?.retentionState === 'pending_delete');
}
