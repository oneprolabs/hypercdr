export type PlanTaskCandidate = { id: string; createdAt?: string };

export function selectPointedPlanTask<T extends PlanTaskCandidate>(candidates: T[], pointedTaskId?: string): T | undefined {
  if (pointedTaskId) return candidates.find(task => task.id === pointedTaskId);
  return [...candidates].sort((left, right) =>
    String(right.createdAt || '').localeCompare(String(left.createdAt || '')) ||
    String(right.id || '').localeCompare(String(left.id || '')),
  )[0];
}
