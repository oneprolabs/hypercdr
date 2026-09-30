import { getUserTimeZone } from './date-time';

export type ScheduleParts = { hour: number; minute: number; weekDay: number; monthDay: number };
type ScheduleInput = ScheduleParts & { type: 'interval' | 'daily' | 'weekly' | 'monthly' };

const partsInZone = (date: Date, timeZone: string) => {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).formatToParts(date);
  const value = (type: string) => Number(parts.find(part => part.type === type)?.value || 0);
  return { year: value('year'), month: value('month'), day: value('day'), hour: value('hour') % 24, minute: value('minute'), second: value('second') };
};

const zonedWallTimeToUTC = (year: number, month: number, day: number, hour: number, minute: number, timeZone: string) => {
  const desired = Date.UTC(year, month - 1, day, hour, minute, 0);
  let result = new Date(desired);
  for (let attempt = 0; attempt < 3; attempt++) {
    const actual = partsInZone(result, timeZone);
    const correction = desired - Date.UTC(actual.year, actual.month - 1, actual.day, actual.hour, actual.minute, 0);
    if (correction === 0) break;
    result = new Date(result.getTime() + correction);
  }
  return result;
};

export function scheduleDisplayToUTC(policy: ScheduleInput, now = new Date()): ScheduleParts {
  if (policy.type === 'interval') return { hour: policy.hour, minute: policy.minute, weekDay: policy.weekDay, monthDay: policy.monthDay };
  const timeZone = getUserTimeZone();
  const local = partsInZone(now, timeZone);
  let { year, month, day } = local;
  if (policy.type === 'weekly') {
    const current = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
    day += (policy.weekDay - current + 7) % 7;
  } else if (policy.type === 'monthly') day = Math.min(policy.monthDay, new Date(Date.UTC(year, month, 0)).getUTCDate());
  const utc = zonedWallTimeToUTC(year, month, day, policy.hour, policy.minute, timeZone);
  let monthDay = utc.getUTCDate();
  if (policy.type === 'monthly' && (utc.getUTCFullYear() * 12 + utc.getUTCMonth() + 1) < year * 12 + month) monthDay = 31;
  return { hour: utc.getUTCHours(), minute: utc.getUTCMinutes(), weekDay: utc.getUTCDay(), monthDay };
}

export function scheduleUTCToDisplay(policy: { scheduleType: string; hour?: number; minute?: number; weekDay?: number; monthDay?: number }, now = new Date()): ScheduleParts {
  if (!['daily', 'weekly', 'monthly'].includes(policy.scheduleType)) return { hour: policy.hour || 0, minute: policy.minute || 0, weekDay: policy.weekDay || 0, monthDay: policy.monthDay || 1 };
  let year = now.getUTCFullYear(), month = now.getUTCMonth(), day = now.getUTCDate();
  if (policy.scheduleType === 'weekly') day += ((policy.weekDay || 0) - now.getUTCDay() + 7) % 7;
  else if (policy.scheduleType === 'monthly') day = Math.min(policy.monthDay || 1, new Date(Date.UTC(year, month + 1, 0)).getUTCDate());
  const local = partsInZone(new Date(Date.UTC(year, month, day, policy.hour || 0, policy.minute || 0)), getUserTimeZone());
  const lastDay = new Date(Date.UTC(local.year, local.month, 0)).getUTCDate();
  return { hour: local.hour, minute: local.minute, weekDay: new Date(Date.UTC(local.year, local.month - 1, local.day)).getUTCDay(), monthDay: policy.scheduleType === 'monthly' && local.day === lastDay ? 31 : local.day };
}
