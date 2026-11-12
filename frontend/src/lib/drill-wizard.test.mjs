import test from 'node:test';import assert from 'node:assert/strict';
import {drillNamespaceError,drillScopeError} from './drill-wizard.ts';
import { readFileSync } from 'node:fs';

test('protection configuration preserves an explicitly empty default target', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /targetClusterId: targetCluster\?\.id \|\| ''/);
  assert.match(page, /cluster\.id === plan\.targetClusterId/);
  assert.doesNotMatch(page, /plan\.targetClusterId \|\| plan\.sourceClusterId/);
  assert.doesNotMatch(page, /targetClusterId: targetCluster\?\.id \|\| currentClusterId/);
  const wizard = readFileSync(new URL('../protect-applications-wizard.tsx', import.meta.url), 'utf8');
  assert.match(wizard, /props\.step === 2 && \(!props\.protectConfig\.targetCluster \|\|/);
  assert.match(wizard, /checked=\{!props\.protectConfig\.targetCluster\}/);
  const settings = readFileSync(new URL('../dr-configuration-modal.tsx', import.meta.url), 'utf8');
  assert.match(settings, /<option value="">No default target<\/option>/);
  assert.doesNotMatch(settings, /protectConfig\.targetCluster \|\| targetClusterOptions\.length === 0/);
});

test('protection configuration can skip or clear a policy without showing a default schedule', () => {
  const wizard = readFileSync(new URL('../protect-applications-wizard.tsx', import.meta.url), 'utf8');
  assert.match(wizard, /props\.step === 4 && \(!props\.protectConfig\.policy \|\| props\.protectConfig\.policy === 'manual'/);
  assert.match(wizard, /No policy \(manual only\)/);
  assert.match(wizard, /policy: 'manual'/);
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /policyId = policies\.some/);
  assert.match(page, /policy: selectedPolicy\?\.name \|\| ''/);
  assert.doesNotMatch(page, /Daily Incremental/);
});

test('restore points do not invent a recovery target and require an explicit execution target', () => {
  const page = readFileSync(new URL('../features/restore-points/restore-point-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /targetCluster: targetCluster\?\.name \|\| ''/);
  assert.match(page, /const targetCluster = row\.targetCluster;/);
  assert.doesNotMatch(page, /clusters\.find\(item => item\.id !== point\.sourceClusterId\)/);
  assert.match(page, /if \(!targetCluster\) \{\s*toast\('Select a target cluster before starting recovery'\);\s*return;/);
  assert.match(page, /clusterId: targetCluster\.id/);
});
test('drill validates Kubernetes namespace names',()=>{for(const name of ['','Uppercase','with space','-bad','bad-', 'x'.repeat(64)])assert.ok(drillNamespaceError(name));for(const name of ['demo-mysql-csi-drill','a','db-123'])assert.equal(drillNamespaceError(name),'');});
test('custom restore cannot omit every resource or bypass failed inspection',()=>{assert.ok(drillScopeError('exclude',['pods','services'],['pods','services'],true,false,''));assert.equal(drillScopeError('exclude',['pods'],['pods','services'],true,false,''),'');assert.ok(drillScopeError('exclude',[],[],false,false,''));assert.ok(drillScopeError('all',[],[],true,false,'failed'));assert.ok(drillScopeError('all',[],[],false,true,''));});
