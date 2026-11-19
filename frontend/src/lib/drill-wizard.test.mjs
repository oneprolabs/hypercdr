import test from 'node:test';import assert from 'node:assert/strict';
import {drillSteps,drillNamespaceError,drillScopeError,recoveryNamespaceMappings,recoveryNamespaceMappingError} from './drill-wizard.ts';
import { readFileSync } from 'node:fs';

test('namespace mappings follow the selected sources in original and clone modes', () => {
  const config = { namespaceMode: 'original', targetNamespace: 'first', restorePackageNamespaces: ['first', 'second'] };
  assert.deepEqual(recoveryNamespaceMappings(['second'], config), [{ source: 'second', target: 'second' }]);
  assert.deepEqual(recoveryNamespaceMappings(['first', 'second'], config), [{ source: 'first', target: 'first' }, { source: 'second', target: 'second' }]);
  const clone = { ...config, namespaceMode: 'generated', targetNamespaces: { second: 'second-test' } };
  assert.deepEqual(recoveryNamespaceMappings(['second'], clone), [{ source: 'second', target: 'second-test' }]);
  assert.deepEqual(recoveryNamespaceMappings(['first', 'second'], clone), [{ source: 'first', target: 'first-drill' }, { source: 'second', target: 'second-test' }]);
  assert.deepEqual(recoveryNamespaceMappings(['second'], { namespaceMode: 'generated', targetNamespace: 'first-drill' }), [{ source: 'second', target: 'second-drill' }]);
  assert.deepEqual(recoveryNamespaceMappings(['second'], { namespaceMode: 'custom', targetNamespace: 'custom-target' }), [{ source: 'second', target: 'custom-target' }]);
});

test('namespace mapping validation rejects invalid or duplicate targets', () => {
  assert.ok(recoveryNamespaceMappingError([{ source: 'one', target: '' }]));
  assert.ok(recoveryNamespaceMappingError([{ source: 'one', target: 'same' }, { source: 'two', target: 'same' }]));
  assert.equal(recoveryNamespaceMappingError([{ source: 'one', target: 'one-drill' }, { source: 'two', target: 'two-drill' }]), '');
});

test('drill keeps three steps and a compact restore screen without duplicate metadata', () => {
  assert.deepEqual(drillSteps.map(step => step[0]), ['Restore', 'Target', 'Review']);
  const wizard = readFileSync(new URL('../recovery-wizard-modal.tsx', import.meta.url), 'utf8');
  assert.doesNotMatch(wizard, /hbdr-drill-point-meta/);
  assert.match(wizard, /hbdr-drill-namespace-choices/);
  assert.match(wizard, /drillStep !== 0 && <div className="hbdr-drill-step-heading"/);
  assert.match(wizard, /useState\(false\)/);
  assert.match(wizard, /aria-expanded=\{advancedOpen\}/);
  assert.match(wizard, /type="checkbox"\s+checked=\{advancedOpen\}/);
  assert.match(wizard, /aria-label="Enable advanced options"/);
  assert.doesNotMatch(wizard, /advancedOpen \? '−' : '\+'/);
  const review = wizard.slice(wizard.indexOf('className="hbdr-protect-review-groups hbdr-drill-review"'), wizard.indexOf('{submitError &&'));
  assert.deepEqual([...review.matchAll(/<h4><span>\d<\/span>([^<]+)<\/h4>/g)].map(match => match[1]), ['Restore', 'Target']);
  assert.ok(review.indexOf('<dt>Restore scope</dt>') < review.indexOf('<span>2</span>Target'));
  assert.ok(review.indexOf('<dt>Advanced options</dt>') < review.indexOf('<span>2</span>Target'));
  assert.doesNotMatch(wizard, /<span><em className="hbdr-protect-target">\{app\.name\}<\/em><\/span>/);
});

test('stage transitions wait for persistence without refreshing the whole platform', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  const start = page.indexOf('const moveAppsToStage =');
  const end = page.indexOf('const handlePrimaryAction =', start);
  assert.ok(start >= 0 && end > start);
  const transition = page.slice(start, end);
  assert.match(transition, /await Promise\.all\(targetApps\.map/);
  assert.match(transition, /updateApplicationProtection/);
  assert.match(transition, /setAppUiOverrides/);
  assert.doesNotMatch(transition, /refreshPlatformData|requestClusterInventory|listTasks/);
  assert.doesNotMatch(page, /dr_support_auto_check|drSupportAutoRequestedRef/);
});

test('application activity polling includes restore points for live row updates', () => {
  const snapshots = readFileSync(new URL('../app/platform-snapshots.ts', import.meta.url), 'utf8');
  assert.match(snapshots, /includeRestorePoints = true/);
  assert.match(snapshots, /includeRestorePoints \? listRestorePoints/);
  const resources = readFileSync(new URL('../app/use-platform-resources.ts', import.meta.url), 'utf8');
  assert.match(resources, /loadApplicationActivitySnapshot\([^\n]+true\);/);
  assert.match(resources, /const loadApplicationRecoveryData = useCallback/);
  assert.match(resources, /const realtimeViews = new Set<string>\(\[[^\]]*'applications'/);
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /const completedPoint = taskVisibleRestorePoint \|\| latestPoint/);
  assert.match(page, /completedSyncTaskKey, loadApplicationRecoveryData/);
});

test('removed plans never fall back to configuring and cleanup clears activation hints', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /if \(plan && \['pending_activation', 'configuring', 'activating_storage', 'activating_schedule'\]\.includes/);
  const cleanup = page.slice(page.indexOf('const deleteDrConfiguration ='), page.indexOf('const cleanupDrillResources ='));
  assert.match(cleanup, /setConfiguringPlanIds\(previous => previous\.filter/);
  assert.match(cleanup, /setConfiguringAppNames\(previous => previous\.filter/);
  assert.match(cleanup, /protectionPlanId: ''/);
  const taskUi = readFileSync(new URL('../features/recovery/task-ui.tsx', import.meta.url), 'utf8');
  assert.match(taskUi, /case '':\s*return \{ label: 'Not configured', tone: 'muted'/);
});

test('cleanup failure is authoritative and force abort targets active tasks', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /cleaningPlanIds\.includes\(app\.protectionPlanId\) && plan\?\.status !== 'cleanup_failed'/);
  assert.match(page, /const failedPlans = remainingPlans\.filter\(plan => plan\.status === 'cleanup_failed'\)/);
  assert.match(page, /Force Abort/);
  assert.doesNotMatch(page, /Cancel Sync/);
  assert.match(page, /forceAbortCandidates\(/);
  assert.match(page, /targets\.length === 0/);
  assert.match(page, /setAbortSelectedIds\(\[\]\)/);
  assert.match(page, /targets\.map\(task => abortTask\(task.id\)\)/);
  const polling = readFileSync(new URL('../features/applications/use-dr-task-events.ts', import.meta.url), 'utf8');
  assert.match(polling, /isActiveTaskStatus\(latest.status\)/);
  assert.doesNotMatch(polling, /displayedDrTaskKey|ids\.map/);
});

test('force cleanup is opt-in and never bypasses active task protection', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /\[forceCleanup, setForceCleanup\] = useState\(false\)/);
  assert.match(page, /deleteProtectionPlan\(planId, forceCleanup\)/);
  assert.match(page, /if \(selectedRunHasActiveTasks\)/);
  assert.match(page, /Remote data may remain/);
  const api = readFileSync(new URL('../api/protection-plans.ts', import.meta.url), 'utf8');
  assert.match(api, /force = false/);
  assert.match(api, /\?force=true/);
});

test('protection configuration preserves an explicitly empty default target', () => {
  const page = readFileSync(new URL('../features/applications/application-dr-page.tsx', import.meta.url), 'utf8');
  assert.match(page, /targetClusterId: targetCluster\?\.id \|\| ''/);
  assert.match(page, /cluster\.id === plan\.targetClusterId/);
  assert.doesNotMatch(page, /plan\.targetClusterId \|\| plan\.sourceClusterId/);
  assert.doesNotMatch(page, /targetClusterId: targetCluster\?\.id \|\| currentClusterId/);
  const wizard = readFileSync(new URL('../protect-applications-wizard.tsx', import.meta.url), 'utf8');
  assert.match(wizard, /props\.step === 2 && \(\(!props\.protectConfig\.targetCluster && noTargetSelected\) \|\|/);
  assert.match(wizard, /checked=\{!props\.protectConfig\.targetCluster && noTargetSelected\}/);
  assert.match(wizard, /\[noTargetSelected, setNoTargetSelected\] = useState\(false\)/);
  assert.match(wizard, /props\.step === 3 && Boolean\(selectedRepo\)/);
  assert.match(page, /storageId: '',\s*policy: ''/);
  const settings = readFileSync(new URL('../dr-configuration-modal.tsx', import.meta.url), 'utf8');
  assert.match(settings, /<option value="">No default target<\/option>/);
  assert.doesNotMatch(settings, /protectConfig\.targetCluster \|\| targetClusterOptions\.length === 0/);
});

test('protection configuration requires an explicit policy or manual-only choice without a default schedule', () => {
  const wizard = readFileSync(new URL('../protect-applications-wizard.tsx', import.meta.url), 'utf8');
  assert.match(wizard, /props\.step === 4 && \(props\.protectConfig\.policy === 'manual' \|\| Boolean\(selectedPolicy\)\)/);
  assert.match(wizard, /checked=\{props\.protectConfig\.policy === 'manual'\}/);
  assert.match(wizard, /<strong>No policy<\/strong>/);
  assert.match(wizard, /Run backups manually\. You can assign a policy later\./);
  assert.match(wizard, /<strong>No target<\/strong>/);
  assert.match(wizard, /Select a target cluster when running a Drill\./);
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
