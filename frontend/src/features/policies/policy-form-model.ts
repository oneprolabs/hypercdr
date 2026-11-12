import { scheduleDisplayToUTC } from '../../lib/policy-schedule.ts';
import { apiPost } from '../../api/client.ts';
import { getUserTimeZone } from '../../lib/date-time.ts';
import { validateRetentionRules, type RetentionRule } from './retention-model.ts';

export type PolicyComposition = 'manual' | 'combined' | 'schedule' | 'retention';
export type PolicyScheduleType = 'interval' | 'daily' | 'weekly' | 'monthly';
export type PolicyForm = {
  retentionRules?: RetentionRule[]; retentionTimezone?: string;
  name: string; composition: PolicyComposition; type: PolicyScheduleType;
  intervalValue: number; intervalUnit: 'minutes' | 'hours'; hour: number; minute: number;
  weekDay: number; monthDay: number; retention: number;
};

export const defaultPolicyForm = (): PolicyForm => ({ name: '', composition: 'combined', type: 'interval', intervalValue: 5, intervalUnit: 'minutes', hour: 0, minute: 0, weekDay: 0, monthDay: 1, retention: 7 });

export const buildPolicyInput = (form: PolicyForm) => {
  const normalized = { ...form, name: form.name.trim(), intervalValue: Math.max(1, Number(form.intervalValue) || 1), retention: Math.max(1, Number(form.retention) || 1) };
  if (!normalized.name) throw new Error('Enter a policy name.');
  if (!Number.isInteger(form.retention) || form.retention < 1) throw new Error('Restore point quota must be a positive integer.');
  const retentionRules = validateRetentionRules(form.composition === 'schedule' ? [] : form.retentionRules || []);
  const utc = scheduleDisplayToUTC(normalized);
  return { name: normalized.name, composition: normalized.composition, scheduleType: normalized.type, intervalValue: normalized.intervalValue, intervalUnit: normalized.intervalUnit, hour: utc.hour, minute: utc.minute, weekDay: utc.weekDay, monthDay: utc.monthDay, retentionCount: normalized.retention, retentionDays: 0, retentionRules, retentionTimezone: form.retentionTimezone || getUserTimeZone(), status: 'active' };
};
export const createPolicy = <T,>(form: PolicyForm): Promise<T> => apiPost<T>('/api/v1/policies', buildPolicyInput(form));
