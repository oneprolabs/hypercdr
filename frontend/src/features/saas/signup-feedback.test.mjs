import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PublicAuthError, signupCooldownSeconds, signupFeedback } from './signup-feedback.ts';

test('existing email provides account links without a successful-send cooldown', () => {
  const result = signupFeedback(new PublicAuthError('Already registered', 'email_already_registered', 60));
  assert.equal(result.showAccountLinks, true);
  assert.equal(result.cooldown, 0);
  assert.equal(result.accountCreated, false);
});
test('delivery errors allow retry and preserve their message', () => {
  const result = signupFeedback(new PublicAuthError('Email could not be sent', 'email_delivery_failed'));
  assert.equal(result.message, 'Email could not be sent');
  assert.equal(result.cooldown, 0);
});
test('rate limiting uses the server retry duration', () => {
  assert.equal(signupFeedback(new PublicAuthError('Wait', 'too_many_attempts', 125)).cooldown, 125);
});
test('expired links offer a fresh request', () => {
  assert.equal(signupFeedback(new PublicAuthError('Expired', 'link_invalid')).showNewLink, true);
});
test('post-registration session failure directs to login rather than registration', () => {
  const result = signupFeedback(new PublicAuthError('Account created', 'account_created_login_failed'));
  assert.equal(result.accountCreated, true);
  assert.equal(result.showAccountLinks, true);
});
test('resend cooldown is based on elapsed time rather than timer tick count', () => {
  assert.equal(signupCooldownSeconds(60000, 0), 60);
  assert.equal(signupCooldownSeconds(60000, 59500), 1);
  assert.equal(signupCooldownSeconds(60000, 61000), 0);
});
