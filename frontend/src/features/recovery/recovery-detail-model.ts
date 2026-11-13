import { parseUTCInstant } from '../../lib/date-time.ts';
import { isActiveTaskStatus } from './task-status.ts';
import type { ApiTask } from './types.ts';

export function recoveryNamespaces(task: ApiTask, side: 'source' | 'target'): string[] {
  const payload = task.payload || {};
  const multiple = payload[`${side}Namespaces`];
  const values = [payload[`${side}Namespace`], ...(Array.isArray(multiple) ? multiple : multiple && typeof multiple === 'object' ? (side === 'source' ? Object.keys(multiple) : Object.values(multiple)) : [])];
  return [...new Set(values.filter((value): value is string => typeof value === 'string' && Boolean(value.trim())).map(value => value.trim()))];
}

export function recoveryDuration(task: ApiTask, now = Date.now()): string {
  const start = parseUTCInstant(task.createdAt)?.getTime();
  const completed = parseUTCInstant(task.completedAt)?.getTime();
  const end = isActiveTaskStatus(task.status) ? now : completed;
  if (start === undefined || end === undefined || end < start) return '—';
  const seconds = Math.floor((end - start) / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor(seconds % 3600 / 60);
  return hours ? `${hours}h ${minutes}m ${seconds % 60}s` : minutes ? `${minutes}m ${seconds % 60}s` : `${seconds}s`;
}
