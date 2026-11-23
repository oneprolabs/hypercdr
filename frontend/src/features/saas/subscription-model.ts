export type SubscriptionPlan = {
  id: string;
  name: string;
  description: string;
  maxClusters: number;
  unlimited?: boolean;
  amountMinor: number;
  currency: string;
  interval: 'month' | 'year';
};

export type SubscriptionSnapshot = {
  provider: string;
  plan: SubscriptionPlan;
  status: string;
  periodStart: string;
  periodEnd: string;
  paidUntil?: string;
  cancelAtPeriodEnd: boolean;
};

export function subscriptionStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    active: 'Active', past_due: 'Payment overdue', canceled: 'Canceled', unpaid: 'Unpaid',
    incomplete: 'Payment incomplete', incomplete_expired: 'Payment expired', trialing: 'Trial', paused: 'Paused',
  };
  return labels[status] || 'Status unavailable';
}

export function subscriptionCheckoutAvailable(subscription: SubscriptionSnapshot | null): boolean {
  return subscription === null || ['canceled', 'incomplete_expired'].includes(subscription.status);
}

export function subscriptionNotice(subscription: SubscriptionSnapshot | null, commercial: boolean): string {
  if (!subscription) return '';
  if (subscription.status === 'past_due') return 'The latest payment is overdue. Update your payment method when subscription management becomes available. Existing tasks are not interrupted.';
  if (subscription.cancelAtPeriodEnd && commercial) return 'Cancellation is scheduled. Your paid cluster capacity remains available until the paid-through date; it then returns to the free one-cluster limit.';
  if (!commercial) return 'No paid subscription capacity is currently effective. The free one-cluster limit applies. Existing clusters and running tasks are not stopped.';
  if (subscription.status === 'canceled') return 'This subscription is canceled. Previously paid capacity remains available until the paid-through date.';
  return '';
}

export function formatSubscriptionPrice(plan: SubscriptionPlan): string {
  const formatter = new Intl.NumberFormat('en-US', { style: 'currency', currency: plan.currency.toUpperCase() });
  const fractionDigits = formatter.resolvedOptions().maximumFractionDigits ?? 2;
  return `${formatter.format(plan.amountMinor / 10 ** fractionDigits)} / ${plan.interval}`;
}

export function formatClusterCapacity(capacity: { maxClusters: number; unlimited?: boolean }): string {
  return capacity.unlimited ? 'Unlimited' : String(capacity.maxClusters);
}
