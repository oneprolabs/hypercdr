export function clearBillingReturn(history: History, href: string): void {
  const url = new URL(href);
  if (!['success', 'canceled', 'return'].includes(url.searchParams.get('billing') || '')) return;
  url.searchParams.delete('billing');
  url.searchParams.delete('session_id');
  history.replaceState(history.state, '', url.href);
}
