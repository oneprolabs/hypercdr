import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, CreditCard, RefreshCw } from 'lucide-react';
import { apiGet, apiPost } from '../../api/client';
import type { FrontendModuleContext } from '../../app/extensions';
import { formatLocalDateTime } from '../../lib/date-time';
import { SettingsHeader } from './organization-settings';
import { LicensePage } from './license-page';
import { clearBillingReturn } from './billing-return';
import { subscriptionStatusLabel, type SubscriptionSnapshot } from './subscription-model';
import './tenant-access.css';

type SubscriptionResponse = {
  mode: 'online' | 'offline';
  usage: {
    source: 'subscription' | 'license'; plan: 'free' | 'commercial'; status: string;
    unlimited?: boolean; validUntil?: string; workerNodes: number; maxWorkerNodes: number;
    workerNodesOverLimit: boolean; workerGracePeriodEndsAt?: string; workerTasksBlocked: boolean;
  };
  subscription: SubscriptionSnapshot | null;
  canManage: boolean;
};
type Quote = { planId: string; quantity: number; amountMinor: number; interval: 'month' | 'year' };
const money = (minor: number) => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 }).format(minor / 100);

export function SubscriptionPage(context: FrontendModuleContext) {
  const [data, setData] = useState<SubscriptionResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [subscribedNodes, setSubscribedNodes] = useState('');
  const [interval, setInterval] = useState<'month' | 'year'>('month');
  const [selected, setSelected] = useState<'flex' | 'business' | null>(null);
  const [quote, setQuote] = useState<Quote | null>(null);
  const [busy, setBusy] = useState(false);
  const [quoteError, setQuoteError] = useState('');
  const request = useRef(0);
  const initialized = useRef(false);
  useEffect(() => { clearBillingReturn(window.history, window.location.href); }, []);
  const load = useCallback(async () => {
    const version = ++request.current;
    setLoading(true);
    setError('');
    try {
      const response = await apiGet<SubscriptionResponse>('/api/v1/saas/subscription');
      if (version !== request.current) return;
      setData(response);
      if (!initialized.current) {
        setSubscribedNodes(String(Math.max(1, response.usage.workerNodes)));
        initialized.current = true;
      }
    } catch (failure) {
      if (version === request.current) setError(failure instanceof Error ? failure.message : 'Could not load subscription.');
    } finally {
      if (version === request.current) setLoading(false);
    }
  }, []);
  useEffect(() => { void load(); return () => { request.current++; }; }, [load]);
  useEffect(() => { setQuote(null); setQuoteError(''); }, [subscribedNodes, data?.usage.workerNodes, interval, selected]);

  if (data?.mode === 'offline') return <LicensePage {...context} />;
  const usage = data?.usage;
  const snapshot = data?.subscription;
  const paid = usage?.plan === 'commercial';
  const knownPlan = snapshot?.plan.id.startsWith('flex-') ? 'Flex' : snapshot?.plan.id.startsWith('business-') ? 'Business' : null;
  const currentPlan = paid ? knownPlan || (usage?.source === 'license' ? 'License' : 'Previous paid plan') : 'Free';
  const quantity = subscribedNodes.trim() === '' ? NaN : Number(subscribedNodes);
  const minimumNodes = Math.max(1, usage?.workerNodes ?? 0);
  const validNodes = Number.isInteger(quantity) && quantity >= minimumNodes && quantity <= 1000000;
  const factor = interval === 'year' ? 10 : 1;
  const flexPrice = validNodes ? quantity * 4900 * factor : null;
  const businessPrice = 490000 * factor;
  const review = async () => {
    if (!selected) return;
    setBusy(true);
    setQuoteError('');
    try {
      setQuote(await apiPost<Quote>('/api/v1/saas/subscription/quote', { planId: `${selected}-${interval === 'year' ? 'annual' : 'monthly'}`, quantity: selected === 'flex' ? quantity : 1 }));
    } catch (failure) {
      setQuoteError(failure instanceof Error ? failure.message : 'Quote could not be confirmed.');
    } finally { setBusy(false); }
  };

  return <section className="ee-organization-access hc-subscription-page">
    <SettingsHeader title="Subscription & billing" description="Manage your plan, node capacity and billing cycle." icon={<CreditCard size={18} />} />
    <div className="ee-settings-panel">
      <header><h4>Current subscription</h4><button className="hbdr-dr-action-secondary" disabled={loading} onClick={() => void load()}><RefreshCw size={14} />Refresh</button></header>
      {error && <p className="ee-settings-error" role="alert">{error} Refresh to retry.</p>}
      {loading && !data && <p role="status">Loading subscription…</p>}
      {usage && <>
        <div className="hc-member-overview">
          <div className="hc-member-identity"><span>Your plan</span><div><strong>{currentPlan}</strong><span className="hc-subscription-label">{paid && snapshot ? subscriptionStatusLabel(snapshot.status) : paid ? 'Licensed' : 'Free account'}</span></div><small>{paid ? 'Paid membership' : 'Free forever · No credit card required'}</small></div>
          <div><span>Worker node usage</span><strong>{usage.workerNodes} <small>/ {usage.unlimited ? 'Unlimited' : usage.maxWorkerNodes} nodes</small></strong>{!usage.unlimited && <progress aria-label="Worker node usage" value={Math.min(usage.workerNodes, usage.maxWorkerNodes)} max={Math.max(1, usage.maxWorkerNodes)} />}<small>{usage.workerNodesOverLimit ? 'Over quota · Review your capacity' : usage.unlimited ? 'Unlimited capacity included' : `${Math.max(0, usage.maxWorkerNodes - usage.workerNodes)} nodes available`}</small></div>
          <div><span>Current billing</span><strong>{paid && snapshot ? `${money(snapshot.plan.amountMinor)} / ${snapshot.plan.interval}` : paid ? 'License-based' : '$0'}</strong><small>{paid && usage.validUntil ? `Paid through ${formatLocalDateTime(usage.validUntil)}` : 'No expiry'}</small></div>
        </div>
        {paid && !knownPlan && <p className="hc-access-note">Your previous subscription is preserved. It has not been migrated or repriced to Flex or Business.</p>}
        {snapshot?.cancelAtPeriodEnd && <p className="hc-access-note">Cancellation scheduled. Confirmed paid access remains until the paid-through date.</p>}
        {usage.workerNodesOverLimit && <p className="hc-access-note hc-access-note-warning" role="status">{usage.workerTasksBlocked ? 'Grace period ended. New Sync and Drill tasks are paused. All other operations remain available.' : `You are over your node allowance. Tasks continue normally during the 30-day grace period${usage.workerGracePeriodEndsAt ? ` until ${formatLocalDateTime(usage.workerGracePeriodEndsAt)}` : ''}. Increase your quota before it ends.`}</p>}
      </>}
    </div>
    {data && <div className="ee-settings-panel">
      <header><div><h4>Available plans</h4><span className="hc-subscription-muted">All plans include unlimited cluster registrations.</span></div><span className="hc-subscription-muted">Prices in USD</span></header>
      <div className="hc-billing-choice" aria-label="Billing cycle"><button aria-pressed={interval === 'month'} className={interval === 'month' ? 'is-active' : ''} disabled={busy} onClick={() => setInterval('month')}>Monthly</button><button aria-pressed={interval === 'year'} className={interval === 'year' ? 'is-active' : ''} disabled={busy} onClick={() => setInterval('year')}>Annual <small>Pay for 10 months · Save 16.7%</small></button></div>
      <div className="hc-subscription-plans hc-worker-plans">
        <article onClick={() => { if (!busy) setSelected(null); }} className={`hc-subscription-plan${currentPlan === 'Free' ? ' is-current' : ''}`}>
          <div className="hc-subscription-plan-heading"><h5>Free</h5>{currentPlan === 'Free' && <span className="hc-subscription-label">Current plan</span>}</div>
          <p>For small environments</p><p className="hc-subscription-price">$0 <small>forever</small></p>
          <ul><li><Check size={14} />Up to 2 worker nodes</li><li><Check size={14} />No payment required</li><li><Check size={14} />Upgrade when you need more nodes</li></ul>
          <footer><button className="hbdr-dr-action-secondary" disabled={busy} onClick={() => setSelected(null)}>{currentPlan === 'Free' ? 'Current plan' : 'View free tier'}</button></footer>
        </article>
        <article onClick={() => { if (!busy) setSelected('flex'); }} className={`hc-subscription-plan${currentPlan === 'Flex' ? ' is-current' : ''}${selected === 'flex' ? ' is-selected' : ''}`}>
          <div className="hc-subscription-plan-heading"><h5>Flex</h5>{currentPlan === 'Flex' && <span className="hc-subscription-label">Current plan</span>}</div>
          <p>Pay for your subscribed node quota</p><p className="hc-subscription-price">{money(4900 * factor)} <small>/ node / {interval}</small></p>
          <ul><li><Check size={14} />All subscribed nodes are charged</li><li><Check size={14} />Reserve capacity for expansion</li><li><Check size={14} />30-day over-quota grace period</li></ul>
          <footer><button className="hbdr-dr-action-secondary" disabled={busy} aria-label="Select Flex plan" aria-pressed={selected === 'flex'} onClick={() => setSelected('flex')}>{selected === 'flex' ? 'Selected' : 'Select plan'}</button></footer>
        </article>
        <article onClick={() => { if (!busy) setSelected('business'); }} className={`hc-subscription-plan${currentPlan === 'Business' ? ' is-current' : ''}${selected === 'business' ? ' is-selected' : ''}`}>
          <div className="hc-subscription-plan-heading"><h5>Business</h5>{currentPlan === 'Business' && <span className="hc-subscription-label">Current plan</span>}</div>
          <p>Fixed price, unlimited nodes</p><p className="hc-subscription-price">{money(businessPrice)} <small>/ {interval}</small></p>
          <ul><li><Check size={14} />Unlimited worker nodes</li><li><Check size={14} />No per-node overage charges</li><li><Check size={14} />Best value above 100 nodes</li></ul>
          <footer><button className="hbdr-dr-action-secondary" disabled={busy} aria-label="Select Business plan" aria-pressed={selected === 'business'} onClick={() => setSelected('business')}>{selected === 'business' ? 'Selected' : 'Select plan'}</button></footer>
        </article>
      </div>
      {selected ? <>
        <div className="hc-plan-config-bar">
          <div className="hc-config-plan"><strong>{selected === 'flex' ? 'Flex' : 'Business'}</strong><span>{interval === 'year' ? 'Annual billing' : 'Monthly billing'}</span></div>
          {selected === 'flex' ? <div className="hc-config-nodes"><div><span>Current usage</span><strong>{usage?.workerNodes ?? 0} nodes</strong></div><label htmlFor="hc-subscribed-nodes"><span>Subscribed nodes</span><input id="hc-subscribed-nodes" type="number" min={minimumNodes} max="1000000" step="1" value={subscribedNodes} disabled={busy} onChange={event => setSubscribedNodes(event.target.value)} aria-describedby="hc-config-help" /></label></div> : <span className="hc-config-unlimited">Unlimited worker nodes</span>}
          <div className="hc-config-price" aria-live="polite"><span>{selected === 'flex' ? `${validNodes ? quantity : '—'} nodes total` : 'Fixed subscription'}</span><strong>{selected === 'business' ? money(businessPrice) : flexPrice === null ? '—' : money(flexPrice)}<small> / {interval}</small></strong></div>
          <button className="hbdr-dr-action-primary" disabled={!data.canManage || busy || (selected === 'flex' && !validNodes)} onClick={() => void review()}>{busy ? 'Checking…' : 'Confirm estimate'}</button>
        </div>
        <p id="hc-config-help" className="hc-subscription-footnote">{selected === 'flex' ? 'We recommend subscribing to extra nodes for future expansion. All subscribed nodes are billed, including the first two.' : 'No per-node charges or capacity limits.'}</p>
        {selected === 'flex' && !validNodes && <p className="ee-settings-error" role="alert">Enter a whole number between {minimumNodes} and 1,000,000. Subscription capacity cannot be lower than current usage.</p>}
      </> : <p className="hc-subscription-footnote">Choose a plan to configure your subscription. Your current plan remains {currentPlan}.</p>}
      {quoteError && <p className="ee-settings-error" role="alert">{quoteError}</p>}
      {quote && <p className="hc-access-note" role="status">Quote confirmed: {money(quote.amountMinor)} / {quote.interval}. Payment is not connected for these plans yet. No subscription was changed and no payment was taken.</p>}
      <p className="hc-subscription-footnote">Checkout is not enabled yet. Reviewing a quote does not change your plan or charge your account.</p>
      {!data.canManage && <p className="hc-access-note">Only organization administrators can manage subscriptions.</p>}
      <details className="hc-subscription-rules"><summary>Node billing and over-quota rules</summary><div><section><h5>Free versus Flex</h5><p>Free includes up to 2 nodes. Once you subscribe to Flex, every subscribed node is charged; the first 2 are not deducted. Annual subscriptions cost 10 monthly payments for a full year.</p></section><section><h5>30-day grace period</h5><p>Tasks continue normally while over quota for 30 days. Afterward, only new Sync and Drill tasks pause. All other operations, including cluster registration and recovery, remain available. Business has no node quota.</p></section></div></details>
    </div>}
  </section>;
}
