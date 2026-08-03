export const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
let activeTimeZone = browserTimeZone;

export function setUserTimeZone(timeZone?: string) {
  activeTimeZone = timeZone || browserTimeZone;
}

export function getUserTimeZone() {
  return activeTimeZone;
}

export function userTimeZoneLabel(timeZone = activeTimeZone) {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, timeZoneName: 'longOffset' }).formatToParts(new Date());
  const offset = parts.find(part => part.type === 'timeZoneName')?.value || 'GMT';
  return `My Time Zone: ${offset} (${timeZone})`;
}

export function parseUTCInstant(value?: string): Date | null {
  if (!value) return null;
  const normalized = value.trim();
  if (!normalized) return null;
  const explicitZone = /(?:z|[+-]\d{2}:?\d{2})$/i.test(normalized);
  const timestamp = new Date(explicitZone || !normalized.includes('T') ? normalized : `${normalized}Z`);
  return Number.isNaN(timestamp.getTime()) ? null : timestamp;
}

export function formatLocalDateTime(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '';
  const parts = new Intl.DateTimeFormat(undefined, {
    timeZone: activeTimeZone,
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value || '';
  return `${part('year')}-${part('month')}-${part('day')} ${part('hour')}:${part('minute')}:${part('second')}`;
}

export function formatDateTime(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '-';
  return date.toLocaleString(undefined, { timeZone: activeTimeZone, month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', second:'2-digit' });
}

export function formatLocalDateKey(value?: string) {
  const date = parseUTCInstant(value);
  if (!date) return '';
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: activeTimeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value || '';
  return `${part('year')}-${part('month')}-${part('day')}`;
}
