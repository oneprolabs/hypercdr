export type NodeResourceKind = 'cpu' | 'memory' | 'storage';
export function resourceQuantity(value?: string): number | undefined {
  if (!value) return undefined;
  const match = value.match(/^([0-9]+(?:\.[0-9]+)?)([EPTGMK]i|[EPTGMkK]|m|u|n)?$/);
  if (!match) return undefined;
  const scale: Record<string, number> = { n: 1e-9, u: 1e-6, m: 1e-3, k: 1e3, K: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18, Ki: 1024, Mi: 1024 ** 2, Gi: 1024 ** 3, Ti: 1024 ** 4, Pi: 1024 ** 5, Ei: 1024 ** 6 };
  const result = Number(match[1]) * (scale[match[2]] ?? 1);
  return Number.isFinite(result) ? result : undefined;
}
function measuredNumber(value?: string) {
  if (value === undefined || value.trim() === '') return undefined;
  const result = Number(value);
  return Number.isFinite(result) && result >= 0 ? result : undefined;
}
export function nodeResource(kind: NodeResourceKind, capacity?: Record<string, string>, usage?: Record<string, string>) {
  const used = measuredNumber(usage?.[kind === 'cpu' ? 'cpuCores' : kind === 'memory' ? 'memoryBytes' : 'storageUsedBytes']);
  const total = kind === 'storage' ? measuredNumber(usage?.storageCapacityBytes) : resourceQuantity(capacity?.[kind]);
  const percent = used !== undefined && total !== undefined && total > 0 ? used / total * 100 : undefined;
  const observedAt = usage?.[`${kind}ObservedAt`];
  const format = (value?: number) => value === undefined ? '—' : kind === 'cpu' ? `${Number(value.toFixed(2))}` : `${(value / 1024 ** 3).toFixed(1)}`;
  return { used, total, percent, observedAt, text: `${format(used)} / ${format(total)} ${kind === 'cpu' ? 'cores' : 'GiB'}` };
}
