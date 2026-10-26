export type RetentionPeriod = 'daily' | 'weekly' | 'monthly' | 'yearly';
export type RetentionRule = { period: RetentionPeriod; ordinal: number; keep: number };

export const retentionUnits: Record<RetentionPeriod, string> = { daily: 'days', weekly: 'weeks', monthly: 'months', yearly: 'years' };

export function validateRetentionRules(rules: RetentionRule[]) {
  if (rules.length > 32) throw new Error('At most 32 automatic retention rules are allowed.');
  const seen = new Set<string>();
  for (const rule of rules) {
    if (!(rule.period in retentionUnits) || !Number.isInteger(rule.ordinal) || rule.ordinal < 1 || rule.ordinal > 100000 || !Number.isInteger(rule.keep) || rule.keep < 1 || rule.keep > 1000) {
      throw new Error('Enter a valid period, point number (1–100000), and retention duration (1–1000).');
    }
    const key = `${rule.period}:${rule.ordinal}:${rule.keep}`;
    if (seen.has(key)) throw new Error('Remove duplicate automatic retention rules.');
    seen.add(key);
  }
  return rules.map(rule => ({ ...rule }));
}

export function retentionRuleSummary(rule: RetentionRule) {
  const period = { daily: 'day', weekly: 'week', monthly: 'month', yearly: 'year' }[rule.period];
  return `Keep successful point #${rule.ordinal} of each ${period} for the last ${rule.keep} ${retentionUnits[rule.period]} (including the current ${period}).`;
}
