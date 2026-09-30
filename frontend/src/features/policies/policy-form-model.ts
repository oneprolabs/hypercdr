import { scheduleDisplayToUTC } from '../../lib/policy-schedule';
import { apiPost } from '../../api/client';

export type PolicyComposition = 'manual' | 'combined' | 'schedule' | 'retention';
export type PolicyScheduleType = 'interval' | 'daily' | 'weekly' | 'monthly';
export type PolicyForm = {
  name: string; composition: PolicyComposition; type: PolicyScheduleType;
  intervalValue: number; intervalUnit: 'minutes' | 'hours'; hour: number; minute: number;
  weekDay: number; monthDay: number; retention: number;
};

export const defaultPolicyForm = (): PolicyForm => ({ name: '', composition: 'combined', type: 'interval', intervalValue: 5, intervalUnit: 'minutes', hour: 0, minute: 0, weekDay: 0, monthDay: 1, retention: 7 });

export const buildPolicyInput = (form: PolicyForm) => {
  const normalized = { ...form, name: form.name.trim(), intervalValue: Math.max(1, Number(form.intervalValue) || 1), retention: Math.max(1, Number(form.retention) || 1) };
  if (!normalized.name) throw new Error('Enter a policy name.');
  const utc = scheduleDisplayToUTC(normalized);
  return { name: normalized.name, composition: normalized.composition, scheduleType: normalized.type, intervalValue: normalized.intervalValue, intervalUnit: normalized.intervalUnit, hour: utc.hour, minute: utc.minute, weekDay: utc.weekDay, monthDay: utc.monthDay, retentionCount: normalized.retention, retentionDays: 0, status: 'active' };
};
export const createPolicy = <T,>(form: PolicyForm): Promise<T> => apiPost<T>('/api/v1/policies', buildPolicyInput(form));
