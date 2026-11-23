import assert from 'node:assert/strict';
import test from 'node:test';
import { formatClusterCapacity, formatSubscriptionPrice, subscriptionCheckoutAvailable, subscriptionNotice, subscriptionStatusLabel } from './subscription-model.ts';

test('subscription prices use currency minor units and the configured billing period', () => {
  assert.equal(formatSubscriptionPrice({ amountMinor: 1299, currency: 'usd', interval: 'month' }), '$12.99 / month');
  assert.equal(formatSubscriptionPrice({ amountMinor: 1200, currency: 'jpy', interval: 'year' }), '¥1,200 / year');
});

test('subscription notices distinguish paid access, scheduled cancellation and failed payment', () => {
  assert.equal(subscriptionNotice(null, false), '');
  assert.match(subscriptionNotice({ status: 'active', cancelAtPeriodEnd: true }, true), /Cancellation is scheduled/);
  assert.match(subscriptionNotice({ status: 'past_due' }, true), /overdue/);
  assert.match(subscriptionNotice({ status: 'canceled' }, false), /free one-cluster/);
  assert.match(subscriptionNotice({ status: 'canceled' }, true), /paid capacity remains/);
  assert.equal(subscriptionStatusLabel('trialing'), 'Trial');
  assert.equal(subscriptionStatusLabel('unknown'), 'Status unavailable');
});

test('approved prices and explicit unlimited capacity remain distinct from zero capacity', () => {
  assert.equal(formatSubscriptionPrice({ amountMinor: 10000, currency: 'usd', interval: 'month' }), '$100.00 / month');
  assert.equal(formatSubscriptionPrice({ amountMinor: 50000, currency: 'usd', interval: 'month' }), '$500.00 / month');
  assert.equal(formatClusterCapacity({ maxClusters: 5 }), '5');
  assert.equal(formatClusterCapacity({ maxClusters: 0, unlimited: true }), 'Unlimited');
  assert.equal(formatClusterCapacity({ maxClusters: 0 }), '0');
});

test('only absent or terminal subscriptions can start another checkout', () => {
  assert.equal(subscriptionCheckoutAvailable(null), true);
  for (const status of ['canceled', 'incomplete_expired']) assert.equal(subscriptionCheckoutAvailable({ status }), true);
  for (const status of ['active', 'past_due', 'unpaid', 'incomplete', 'trialing', 'paused', 'unknown']) assert.equal(subscriptionCheckoutAvailable({ status }), false);
});
