import { useCallback, useEffect, useRef, useState } from 'react';
import { CreditCard, RefreshCw } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { formatLocalDateTime } from '../../lib/date-time';
import { SettingsHeader } from './organization-settings';
import { LicensePage } from './license-page';
import { formatClusterCapacity, formatSubscriptionPrice, subscriptionCheckoutAvailable, subscriptionNotice, subscriptionStatusLabel, type SubscriptionPlan, type SubscriptionSnapshot } from './subscription-model';
import './tenant-access.css';

type SubscriptionResponse = {
  mode: 'online' | 'offline';
  usage: {
    tenantId: string; source: 'subscription' | 'license'; plan: 'free' | 'commercial'; status: string;
    maxClusters: number; unlimited?: boolean; usedClusters: number; overLimit: boolean; canRegister: boolean; validUntil?: string;
  };
  subscription: SubscriptionSnapshot | null;
  plans: SubscriptionPlan[];
  canManage: boolean;
  payment: { configured: boolean; reason: string; testMode?: boolean; verificationDelayed?: boolean };
};

export function SubscriptionPage(context: FrontendModuleContext) {
  const [data, setData] = useState<SubscriptionResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [actionError, setActionError] = useState('');
  const [busy, setBusy] = useState('');
  const actionLock = useRef(false);
  const billingReturn = useRef(new URLSearchParams(window.location.search).get('billing'));
  const request = useRef(0);
  const load = useCallback(async () => {
    const version = ++request.current;
    setLoading(true);
    setError('');
    try {
      const response = await apiGet<SubscriptionResponse>('/api/v1/saas/subscription');
      if (request.current === version) setData(response);
    } catch (failure) {
      if (request.current === version) setError(failure instanceof Error ? failure.message : 'Subscription information could not be loaded.');
    } finally {
      if (request.current === version) setLoading(false);
    }
  }, []);
  useEffect(() => { void load(); return () => { request.current++; }; }, [load]);
  useEffect(() => {
    const restore = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      actionLock.current = false;
      setBusy('');
      void load();
    };
    window.addEventListener('pageshow', restore);
    return () => window.removeEventListener('pageshow', restore);
  }, [load]);
  useEffect(() => {
    if (!['success', 'return'].includes(billingReturn.current || '')) return;
    const timer = window.setInterval(() => { void load(); }, 5000);
    const timeout = window.setTimeout(() => window.clearInterval(timer), 120000);
    return () => { window.clearInterval(timer); window.clearTimeout(timeout); };
  }, [load]);
  const openBilling = async (action: 'checkout' | 'portal', planId?: string) => {
    if (actionLock.current) return;
    actionLock.current = true;
    setBusy(planId || action);
    setActionError('');
    try {
      const response = await apiPost<{ url: string }>(`/api/v1/saas/subscription/${action}`, planId ? { planId } : {});
      const destination = new URL(response.url);
      if (destination.protocol !== 'https:' || !['checkout.stripe.com', 'billing.stripe.com'].includes(destination.host)) throw new Error('Billing returned an invalid destination. Retry shortly.');
      window.location.assign(destination.href);
    } catch (failure) {
      setActionError(failure instanceof Error ? failure.message : 'Billing could not be opened. Retry shortly.');
      actionLock.current = false;
      setBusy('');
    }
  };

  if (data?.mode === 'offline') return <LicensePage {...context} />;
  const usage = data?.usage;
  const commercial = usage?.plan === 'commercial';
  const subscription = usage?.source === 'subscription' ? data?.subscription ?? null : null;
  const checkoutAvailable = subscriptionCheckoutAvailable(subscription);
  const notice = subscriptionNotice(subscription, commercial);
  const planName = commercial ? subscription?.plan.name || 'Commercial license' : 'Free';

  return <section className="ee-organization-access hc-subscription-page">
    {billingReturn.current === 'success' && <p className="hc-access-note" role="status">Checkout returned. Your subscription updates only after payment is confirmed. This can take a moment; refresh if it has not updated.</p>}
    {billingReturn.current === 'canceled' && <p className="hc-access-note" role="status">Checkout canceled. No subscription change was requested.</p>}
    <SettingsHeader title="Subscription" description="Your organization’s plan, shared cluster capacity and billing." icon={<CreditCard size={18} />} />
    <div className="ee-settings-panel">
      <header><h4>Current subscription</h4><button className="hbdr-dr-action-secondary" disabled={loading} onClick={() => void load()}><RefreshCw size={14} />{loading && data ? 'Refreshing…' : 'Refresh'}</button></header>
      {error && <p className="ee-settings-error" role="alert">{error} Use Refresh to retry. {data ? 'Previously loaded information is shown below.' : ''}</p>}
      {actionError && <p className="ee-settings-error" role="alert">{actionError}</p>}
      {data?.payment.verificationDelayed && <p className="hc-access-note hc-access-note-warning" role="status">Payment verification is delayed. We are retrying automatically. Previously confirmed access remains valid until its expiry; use Refresh to check again.</p>}
      {loading && !data && <p role="status">Loading subscription information…</p>}
      {usage && <>
        <div className="hc-subscription-overview">
        <div className="hc-subscription-current"><strong>{planName}</strong><span>{usage.status === 'expired' ? 'Paid access expired' : subscription ? subscriptionStatusLabel(subscription.status) : 'Active'}</span></div>
        <dl className="hc-license-summary">
          <div><dt>Registered clusters</dt><dd>{usage.usedClusters} <span className="hc-subscription-muted">/ {formatClusterCapacity(usage)}</span></dd><small>{usage.unlimited ? 'No cluster limit' : usage.overLimit ? 'Over limit · New registrations blocked' : usage.canRegister ? `${usage.maxClusters - usage.usedClusters} available` : 'Limit reached'}</small></div>
          <div><dt>{commercial ? 'Paid through' : 'Validity'}</dt><dd>{commercial && usage.validUntil ? formatLocalDateTime(usage.validUntil) : 'No expiry'}</dd></div>
          <div><dt>{subscription && !commercial ? 'Subscription billing' : 'Billing'}</dt><dd>{subscription ? formatSubscriptionPrice(subscription.plan) : commercial ? 'License-based' : 'No charges'}</dd>{subscription && !commercial && <small>{subscriptionStatusLabel(subscription.status)} · Separate from current free access</small>}{subscription?.cancelAtPeriodEnd && <small>Cancellation scheduled</small>}</div>
        </dl>
        </div>
        {(usage.overLimit || (usage.status === 'expired' && !notice)) && <p className="hc-access-note hc-access-note-warning" role="status">{usage.overLimit ? 'Cluster limit exceeded. New registrations are blocked.' : 'Paid access expired. The free cluster limit applies.'} Existing clusters and tasks are unaffected.</p>}
        {notice && <p className="hc-access-note hc-access-note-warning" role="status">{notice}</p>}
        {usage.source === 'license' && <p className="hc-access-note">Your existing license is preserved. Online subscription capacity is not added to license capacity.</p>}
      </>}
    </div>
    {data && <div className="ee-settings-panel">
      <header><h4>Subscriptions</h4><span className="hc-subscription-muted hc-subscription-caption">Prices and cluster limits are per organization</span></header>
      <div className="hc-subscription-plans">
        <article className={`hc-subscription-plan${!commercial ? ' is-current' : ''}`}><div className="hc-subscription-plan-heading"><h5>Free</h5>{!commercial && <span className="hc-subscription-label">Current</span>}</div><p className="hc-subscription-price">$0 <small>· No expiry</small></p><div className="hc-subscription-capacity"><strong>1</strong><span>registered cluster</span></div><footer><button className="hbdr-dr-action-secondary" disabled>{commercial ? 'Included free tier' : 'Current subscription'}</button></footer></article>
        {data.plans.map(plan => {
          const current = commercial && subscription?.plan.id === plan.id;
          return <article className={`hc-subscription-plan${current ? ' is-current' : ''}`} key={plan.id}><div className="hc-subscription-plan-heading"><h5>{plan.name}</h5>{current && <span className="hc-subscription-label is-commercial">Current</span>}</div><p className="hc-subscription-price">{formatSubscriptionPrice(plan)} <small>{plan.currency.toUpperCase()}</small></p><div className="hc-subscription-capacity"><strong className={plan.unlimited ? 'is-unlimited' : undefined}>{formatClusterCapacity(plan)}</strong><span>registered clusters</span></div><footer><button className="hbdr-dr-action-secondary" disabled={!data.canManage || !data.payment.configured || !!busy || current} aria-busy={busy === plan.id} onClick={() => { void openBilling(checkoutAvailable ? 'checkout' : 'portal', checkoutAvailable ? plan.id : undefined); }} title={current ? 'This is your current subscription.' : data.canManage ? data.payment.reason : 'Only organization administrators can manage subscriptions.'}>{busy === plan.id ? 'Opening checkout…' : current ? 'Current subscription' : checkoutAvailable ? `Upgrade to ${plan.name}` : 'Manage subscription'}</button></footer></article>;
        })}
        {data.plans.length === 0 && <article className="hc-subscription-plan hc-subscription-plan-pending"><h5>Need more clusters?</h5><p>Commercial subscriptions are not available yet.</p><p className="hc-subscription-muted">Plans and pricing will appear here after they are configured. Your free access remains available.</p></article>}
      </div>
      <p className="hc-subscription-muted hc-subscription-terms">All tiers include backup, recovery, Drill and role-based access.</p>
      <div className="hc-subscription-payment"><div><strong>{data.payment.configured ? 'Sandbox billing' : 'Online billing is not connected'}</strong><span> · {data.payment.configured ? 'Test payments only. No real money is charged.' : 'Purchases are unavailable. Your current access is unchanged.'}</span></div>{data.canManage && subscription && <button className="hbdr-dr-action-secondary" disabled={!data.payment.configured || !!busy} aria-busy={busy === 'portal'} onClick={() => { void openBilling('portal'); }} title={data.payment.reason}>{busy === 'portal' ? 'Opening…' : 'Manage subscription'}</button>}</div>
      {!data.canManage && <p className="ee-invite-description">Only organization administrators can purchase or manage subscriptions. Contact your administrator for additional capacity.</p>}
      <details className="hc-subscription-rules"><summary>Cluster capacity &amp; expiry</summary><div><section><h5>Cluster counting</h5><p>Source, target, offline and unregistering clusters count until unregistration completes.</p></section><section><h5>When paid access expires</h5><p>The limit returns to 1 cluster. Over-limit registrations are blocked; existing clusters and running tasks are unaffected.</p></section></div></details>
    </div>}
  </section>;
}
