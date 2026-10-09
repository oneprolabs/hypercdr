import { useEffect, useMemo, useRef, useState } from 'react';
import { Check, ChevronLeft, ChevronRight, Plus, Search, ShieldCheck, X } from 'lucide-react';
import { apiGet, apiDelete, apiPost } from './api/client';
import type { Props, ProtectWizardStep } from './dr-configuration-modal';
import { ScopedResourceSelector } from './components/scoped-resource-selector';
import { createPolicy, defaultPolicyForm } from './features/policies/policy-form-model';
import { PolicyFormFields } from './features/policies/policy-form-fields';
import { StorageCreateFields } from './features/storage/storage-create-fields';
import { ResourceCreateDrawer } from './components/resource-create-drawer';
import { createStorageDraft, createStorageRepository, storageReady, testStorageDraft as testStorageConnectionDraft, type StorageDraft } from './features/storage/storage-form-model';
import { ClusterRegistrationChoices } from './features/clusters/cluster-registration-choices';
import { ClusterPlatformRegistrationFields } from './features/clusters/cluster-platform-registration-fields';
import { ClusterCommandRegistrationFields } from './features/clusters/cluster-command-registration-fields';
import { uploadRegistrationKubeconfig, inspectRegistrationCluster, startRegistrationTask, cancelRegistrationTask } from './features/clusters/cluster-registration-service';

const steps = [
  ['Scope', 'Entire namespace'],
  ['Target', 'Recovery cluster'],
  ['Storage', 'Backup repository'],
  ['Policy', 'Schedule and retention'],
  ['Hooks', 'Optional'],
  ['Review', 'Confirm configuration'],
] as const;
type RegistrationType = 'native-kubernetes' | 'huaweicloud-cce' | 'openshift';
type KubeconfigUpload = { id: string; contexts: Array<{ name: string; apiServer: string; isCurrent?: boolean }> };
type Inspection = { clusterName: string; storageClasses: string[]; defaultStorageClass?: string; gates: Array<{ id: string; label: string; status: string; detail: string }> };
type RegistrationTask = { id: string; status: string; progress?: number; errorMessage?: string; clusterId?: string };

/** New guided flow; the existing DR configuration modal remains available separately. */
export function ProtectApplicationsWizard(props: Props & { onResourcesChanged: () => Promise<unknown> }) {
  const [policyQuery, setPolicyQuery] = useState('');
  const [targetQuery, setTargetQuery] = useState('');
  const [storageQuery, setStorageQuery] = useState('');
  const [policyPage, setPolicyPage] = useState(1);
  const [inlineCreate, setInlineCreate] = useState<'target' | 'storage' | 'policy' | null>(null);
  const [creationBusy, setCreationBusy] = useState(false);
  const [creationError, setCreationError] = useState('');
  const [newName, setNewName] = useState('');
  const [storageDraft, setStorageDraft] = useState<StorageDraft | null>(null);
  const [storageTestResult, setStorageTestResult] = useState<{ tone: 'ok' | 'fail'; text: string } | null>(null);
  const [policyForm, setPolicyForm] = useState(defaultPolicyForm);
  const [installCommand, setInstallCommand] = useState('');
  const [prepareNodeCommand, setPrepareNodeCommand] = useState('');
  const [commandCopied, setCommandCopied] = useState(false);
  const [registrationWatchStarted, setRegistrationWatchStarted] = useState(false);
  const [caCommandCopied, setCaCommandCopied] = useState(false);
  const installCommandRef = useRef<HTMLTextAreaElement>(null);
  const registryCACommandRef = useRef<HTMLTextAreaElement>(null);
  const [registrationBaseline, setRegistrationBaseline] = useState<string[]>([]);
  const [registrationMode, setRegistrationMode] = useState<'command' | 'platform'>('platform');
  const [registrationType, setRegistrationType] = useState<RegistrationType>('native-kubernetes');
  const [kubeconfigUpload, setKubeconfigUpload] = useState<KubeconfigUpload | null>(null);
  const [kubeconfigContext, setKubeconfigContext] = useState('');
  const [clusterInspection, setClusterInspection] = useState<Inspection | null>(null);
  const [registrationStorageClass, setRegistrationStorageClass] = useState('');
  const [registrationTask, setRegistrationTask] = useState<RegistrationTask | null>(null);
  const refreshedRegistrationTask = useRef('');
  useEffect(() => {
    if (!props.open || inlineCreate !== 'target' || registrationMode !== 'command') return;
    let canceled = false;
    setCreationBusy(true); setCreationError(''); setInstallCommand(''); setPrepareNodeCommand(''); setCommandCopied(false); setRegistrationWatchStarted(false);
    void apiPost<{ installCommand: string; prepareNodeCommand?: string }>('/api/v1/agent-tokens', { description: 'cluster registration from protection wizard', ttlSeconds: 1800, clusterType: registrationType })
      .then(token => {
        if (canceled) return;
        if (!token.installCommand) throw new Error('Install command generation failed.');
        setRegistrationBaseline(props.targetClusterOptions.map(cluster => cluster.id));
        setInstallCommand(token.installCommand); setPrepareNodeCommand(token.prepareNodeCommand || '');
      })
      .catch(error => { if (!canceled) setCreationError(error instanceof Error ? error.message : 'Install command generation failed.'); })
      .finally(() => { if (!canceled) setCreationBusy(false); });
    return () => { canceled = true; };
  }, [props.open, inlineCreate, registrationMode, registrationType]);
  useEffect(() => {
    if (!registrationWatchStarted || !installCommand || !props.open) return;
    const timer = window.setInterval(() => { void props.onResourcesChanged().catch(() => undefined); }, 5000);
    return () => window.clearInterval(timer);
  }, [registrationWatchStarted, installCommand, props.open, props.onResourcesChanged]);
  useEffect(() => {
    if (!registrationWatchStarted || !installCommand || !props.open) return;
    const added = props.targetClusterOptions.find(cluster => !registrationBaseline.includes(cluster.id) && cluster.compatible && cluster.connectionStatus === 'online');
    if (!added) return;
    props.setProtectConfig(prev => ({ ...prev, targetCluster: added.name }));
    setInlineCreate(null); setInstallCommand('');
  }, [registrationWatchStarted, installCommand, props.open, props.targetClusterOptions, registrationBaseline, props.setProtectConfig]);
  useEffect(() => {
    if (!registrationTask || !['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(registrationTask.status)) return;
    const timer = window.setInterval(() => {
      void apiGet<RegistrationTask>(`/api/v1/tasks/${encodeURIComponent(registrationTask.id)}`).then(setRegistrationTask).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [registrationTask?.id, registrationTask?.status]);
  useEffect(() => {
    if (registrationTask?.status !== 'succeeded' || refreshedRegistrationTask.current === registrationTask.id) return;
    refreshedRegistrationTask.current = registrationTask.id;
    void props.onResourcesChanged().catch(() => undefined);
  }, [registrationTask?.id, registrationTask?.status, props.onResourcesChanged]);
  useEffect(() => {
    if (registrationTask?.status !== 'succeeded' || inlineCreate !== 'target') return;
    const timer = window.setInterval(() => { void props.onResourcesChanged().catch(() => undefined); }, 5000);
    return () => window.clearInterval(timer);
  }, [registrationTask?.status, inlineCreate, props.onResourcesChanged]);
  useEffect(() => {
    if (registrationTask?.status !== 'succeeded') return;
    const added = props.targetClusterOptions.find(cluster => cluster.id === registrationTask.clusterId || !registrationBaseline.includes(cluster.id) && cluster.connectionStatus === 'online');
    if (!added || !added.compatible) return;
    props.setProtectConfig(prev => ({ ...prev, targetCluster: added.name }));
    setInlineCreate(null);
  }, [registrationTask?.status, registrationTask?.clusterId, props.targetClusterOptions, registrationBaseline, props.setProtectConfig]);
  const policies = useMemo(() => props.policyOptions.filter(policy =>
    `${policy.name} ${policy.schedule} ${policy.retention}`.toLowerCase().includes(policyQuery.trim().toLowerCase())
  ), [policyQuery, props.policyOptions]);
  const targets = useMemo(() => props.targetClusterOptions.filter(cluster =>
    `${cluster.name} ${cluster.region} ${cluster.version}`.toLowerCase().includes(targetQuery.trim().toLowerCase())
  ), [targetQuery, props.targetClusterOptions]);
  const repositories = useMemo(() => props.storage.filter(repo =>
    `${repo.name} ${repo.type} ${repo.bucket || repo.endpoint || ''}`.toLowerCase().includes(storageQuery.trim().toLowerCase())
  ), [storageQuery, props.storage]);
  if (!props.open) return null;
  const pageSize = 4;
  const pageCount = Math.max(1, Math.ceil(policies.length / pageSize));
  const currentPage = Math.min(policyPage, pageCount);
  const pagePolicies = policies.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const selectedTarget = props.targetClusterOptions.find(cluster => cluster.name === props.protectConfig.targetCluster);
  const selectedRepo = props.storage.find(repo => repo.id === props.protectConfig.storageId);
  const selectedPolicy = props.policyOptions.find(policy => policy.id === props.protectConfig.policy);
  const canNext = props.step === 1 || props.step === 5 || props.step === 6
    || props.step === 2 && Boolean(selectedTarget?.compatible && selectedTarget.connectionStatus === 'online')
    || props.step === 3 && Boolean(selectedRepo)
    || props.step === 4 && Boolean(selectedPolicy);
  const goNext = () => {
    if (!canNext || props.submitting) return;
    if (props.step === 6) props.onFinish();
    else props.setStep((props.step + 1) as ProtectWizardStep);
  };
  const openCreate = (kind: 'target' | 'storage' | 'policy') => {
    setInlineCreate(kind); setCreationBusy(false); setCreationError(''); setNewName(''); if (kind === 'policy') setPolicyForm(defaultPolicyForm()); if (kind === 'storage') { setStorageDraft(createStorageDraft('S3-Compatible')); setStorageTestResult(null); } if (kind === 'target') { setRegistrationMode('platform'); setRegistrationType('native-kubernetes'); setInstallCommand(''); setPrepareNodeCommand(''); setCommandCopied(false); setRegistrationWatchStarted(false); setCaCommandCopied(false); setKubeconfigUpload(null); setKubeconfigContext(''); setClusterInspection(null); setRegistrationStorageClass(''); setRegistrationTask(null); }
  };
  const testStorageDraft = async () => {
    if (!storageDraft) return;
    setCreationBusy(true); setStorageTestResult(null);
    try {
      setStorageTestResult(await testStorageConnectionDraft(storageDraft));
    } catch (error) { setStorageTestResult({ tone: 'fail', text: error instanceof Error ? error.message : 'Test connection failed' }); }
    finally { setCreationBusy(false); }
  };
  const uploadKubeconfig = async (file?: File) => {
    if (!file) return;
    setCreationBusy(true); setCreationError(''); setClusterInspection(null); setRegistrationTask(null);
    try {
      const upload = await uploadRegistrationKubeconfig<KubeconfigUpload>(file, registrationType);
      setKubeconfigUpload(upload);
      setKubeconfigContext(upload.contexts.find(context => context.isCurrent)?.name || (upload.contexts.length === 1 ? upload.contexts[0].name : ''));
    } catch (error) { setCreationError(error instanceof Error ? error.message : 'Kubeconfig upload failed.'); }
    finally { setCreationBusy(false); }
  };
  const inspectCluster = async () => {
    if (!kubeconfigUpload || !kubeconfigContext) return;
    setCreationBusy(true); setCreationError('');
    try {
      const result = await inspectRegistrationCluster<Inspection>(kubeconfigUpload.id, kubeconfigContext, registrationType);
      setClusterInspection(result);
      setRegistrationStorageClass(result.defaultStorageClass || (result.storageClasses.length === 1 ? result.storageClasses[0] : ''));
    } catch (error) { setCreationError(error instanceof Error ? error.message : 'Cluster inspection failed.'); }
    finally { setCreationBusy(false); }
  };
  const registerCluster = async () => {
    if (!kubeconfigUpload || !kubeconfigContext || !clusterInspection || !registrationStorageClass) return;
    setCreationBusy(true); setCreationError(''); setRegistrationBaseline(props.targetClusterOptions.map(cluster => cluster.id));
    try {
      setRegistrationTask(await startRegistrationTask<RegistrationTask>(kubeconfigUpload.id, kubeconfigContext, registrationStorageClass, registrationType, `wizard-${crypto.randomUUID()}`));
    } catch (error) { setCreationError(error instanceof Error ? error.message : 'Cluster registration failed to start.'); }
    finally { setCreationBusy(false); }
  };
  const cancelRegistration = async () => {
    if (!registrationTask) return;
    setCreationBusy(true); setCreationError('');
    try {
      setRegistrationTask(await cancelRegistrationTask<RegistrationTask>(registrationTask.id));
    } catch (error) { setCreationError(error instanceof Error ? error.message : 'Could not cancel registration.'); }
    finally { setCreationBusy(false); }
  };
  const closeInline = (continueInBackground = false) => {
    if (registrationTask && ['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(registrationTask.status)) {
      setCreationError('Registration is still running. Wait for completion or cancel it first.');
      return;
    }
    if (kubeconfigUpload?.id && (!registrationTask || ['succeeded', 'failed', 'canceled', 'cancelled'].includes(registrationTask.status))) void apiDelete(`/api/v1/cluster-registrations/kubeconfigs/${encodeURIComponent(kubeconfigUpload.id)}`).catch(() => { /* Temporary uploads also expire server-side. */ });
    const keepWaitingForCommand = inlineCreate === 'target' && registrationMode === 'command' && Boolean(installCommand) && (registrationWatchStarted || continueInBackground);
    setRegistrationWatchStarted(keepWaitingForCommand);
    setKubeconfigUpload(null); setKubeconfigContext(''); setClusterInspection(null); setRegistrationStorageClass(''); setRegistrationTask(null); if (!keepWaitingForCommand) { setInstallCommand(''); setPrepareNodeCommand(''); } setStorageDraft(null); setCreationBusy(false); setInlineCreate(null);
  };
  const closeWizard = () => {
    if (inlineCreate) { closeInline(); if (registrationTask && ['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(registrationTask.status)) return; }
    props.onClose();
  };
  const createResource = async () => {
    if (!inlineCreate || creationBusy) return;
    setCreationBusy(true); setCreationError('');
    try {
      if (inlineCreate === 'target') {
        setRegistrationWatchStarted(false); setCommandCopied(false); setInstallCommand('');
        setRegistrationBaseline(props.targetClusterOptions.map(cluster => cluster.id));
        const token = await apiPost<{ installCommand: string; prepareNodeCommand?: string }>('/api/v1/agent-tokens', { description: 'cluster registration from protection wizard', ttlSeconds: 1800, clusterType: registrationType });
        if (!token.installCommand) throw new Error('Install command generation failed.');
        setInstallCommand(token.installCommand); setPrepareNodeCommand(token.prepareNodeCommand || '');
      } else if (inlineCreate === 'storage') {
        if (!storageReady(storageDraft)) throw new Error('Complete the required repository fields.');
        const created = await createStorageRepository<{ id: string }>(storageDraft!);
        props.setProtectConfig(prev => ({ ...prev, storageId: created.id }));
        await props.onResourcesChanged();
        setStorageDraft(null); setInlineCreate(null);
      } else {
        const created = await createPolicy<{ id: string }>(policyForm);
        props.setProtectConfig(prev => ({ ...prev, policy: created.id }));
        await props.onResourcesChanged();
        setInlineCreate(null);
      }
    } catch (error) { setCreationError(error instanceof Error ? error.message : 'Creation failed.'); }
    finally { setCreationBusy(false); }
  };
  return <div className="hbdr-protect-wizard-backdrop" role="presentation">
    <div className="hbdr-protect-wizard" role="dialog" aria-modal="true" aria-labelledby="hbdr-protect-wizard-title">
      <header className="hbdr-protect-wizard-header">
        <span className="hbdr-protect-wizard-mark"><ShieldCheck size={22} /></span>
        <div><h2 id="hbdr-protect-wizard-title">Protect applications</h2><p>{props.targetNames.join(', ')} · {props.targetCount} namespace{props.targetCount === 1 ? '' : 's'}</p></div>
        <button type="button" aria-label="Close protect applications" onClick={closeWizard}><X size={20} /></button>
      </header>
      <div className="hbdr-protect-wizard-body">
        <nav className="hbdr-protect-wizard-steps" aria-label="Protection setup steps">
          {steps.map(([label, detail], index) => {
            const number = (index + 1) as ProtectWizardStep;
            return <button key={label} type="button" className={props.step === number ? 'is-current' : ''} onClick={() => { if (number < props.step) props.setStep(number); }} disabled={number > props.step} aria-current={props.step === number ? 'step' : undefined}>
              <span className={number < props.step ? 'is-done' : ''}>{number < props.step ? <Check size={15} /> : number}</span>
              <span><strong>{label}</strong><small>{detail}</small></span>
            </button>;
          })}
        </nav>
        <main className="hbdr-protect-wizard-content">
          {props.step === 1 && <section><h3>Application scope</h3><p>Review the namespaces and choose which resources this protection plan will include.</p>{props.targetCount > 1 && <div className="hbdr-protect-wizard-scope-summary"><div className="hbdr-protect-wizard-summary"><strong>{props.targetSummary}</strong><span>{props.targetNames.join(', ')}</span></div><fieldset className="hbdr-protect-wizard-scope-mode"><legend>Protection mode</legend><label><input type="radio" name="wizard-scope-mode" checked={!props.protectConfig.mergeNamespaces} onChange={() => props.setProtectConfig(prev => ({ ...prev, mergeNamespaces: false }))}/><span><strong>Independent</strong><small>Protect each namespace as its own application scope.</small></span></label><label><input type="radio" name="wizard-scope-mode" checked={Boolean(props.protectConfig.mergeNamespaces)} onChange={() => props.setProtectConfig(prev => ({ ...prev, mergeNamespaces: true, scope: 'all', includeAllResources: true, includedResources: [], excludedResources: [], labelSelector: { matchLabels: {}, matchExpressions: [] }, resourceSelection: { mode: 'all', namespaceScoped: [], clusterScoped: [] } }))}/><span><strong>Merge</strong><small>Protect all selected namespaces together as one scope.</small></span></label></fieldset></div>}{props.targetCount > 1 ? <p>Multi-namespace protection always includes all application resources.</p> : <ScopedResourceSelector purpose="backup" value={props.protectConfig.resourceSelection} onChange={resourceSelection => props.setProtectConfig(prev => ({ ...prev, scope: 'all', includeAllResources: resourceSelection.mode === 'all', resourceSelection, includedResources: [], excludedResources: [], labelSelector: { matchLabels: {}, matchExpressions: [] } }))} namespaceResources={props.namespaceResourceOptions || []} customResourcesLoaded={props.customResourcesLoaded} onRequestCustomResources={props.onRequestCustomResources} />}</section>}
          {props.step === 2 && <section><h3>Target cluster</h3><p>Choose a compatible recovery cluster. Registration can be started without leaving this wizard.</p><div className="hbdr-protect-wizard-selection-tools"><label><Search size={17}/><input value={targetQuery} onChange={event => setTargetQuery(event.target.value)} placeholder="Search clusters" /></label><div><button type="button" onClick={() => void props.onResourcesChanged()}>Refresh</button><button type="button" onClick={() => openCreate('target')}><Plus size={15} />Register cluster</button></div></div><div className="hbdr-protect-wizard-options">{targets.map(cluster => <label key={cluster.id} className={props.protectConfig.targetCluster === cluster.name ? 'is-selected' : ''}><input type="radio" name="target-cluster" checked={props.protectConfig.targetCluster === cluster.name} disabled={!cluster.compatible} onChange={() => props.setProtectConfig(prev => ({ ...prev, targetCluster: cluster.name }))}/><span className="hbdr-protect-wizard-card-copy"><span className="hbdr-protect-wizard-card-heading"><strong>{cluster.name}</strong><em>{cluster.connectionStatus || 'Unknown'}</em></span><small title={`${cluster.clusterType || 'Kubernetes'} · ${cluster.version} · ${cluster.region} · ${cluster.nodes} nodes · ${cluster.namespaces ?? '—'} namespaces · ${cluster.applications} applications${cluster.compatible ? '' : ` · ${cluster.incompatibilityReason || 'Incompatible cluster'}`}`}>{cluster.clusterType || 'Kubernetes'} · {cluster.version} · {cluster.region} · {cluster.nodes} nodes · {cluster.namespaces ?? '—'} namespaces · {cluster.applications} applications{!cluster.compatible && ` · ${cluster.incompatibilityReason || 'Incompatible cluster'}`}</small></span></label>)}{targets.length === 0 && <p>No clusters match the search.</p>}</div><div className="hbdr-protect-wizard-pagination"><span>Showing {targets.length} of {props.targetClusterOptions.length} clusters</span></div></section>}
          {props.step === 3 && <section><h3>Backup repository</h3><p>Select a repository for recovery points or create one within this flow.</p><div className="hbdr-protect-wizard-selection-tools"><label><Search size={17}/><input value={storageQuery} onChange={event => setStorageQuery(event.target.value)} placeholder="Search repositories" /></label><div><button type="button" onClick={() => openCreate('storage')}><Plus size={15} />New storage</button></div></div><div className="hbdr-protect-wizard-options">{repositories.map(repo => <label key={repo.id} className={props.protectConfig.storageId === repo.id ? 'is-selected' : ''}><input type="radio" name="storage-repo" checked={props.protectConfig.storageId === repo.id} onChange={() => props.setProtectConfig(prev => ({ ...prev, storageId: repo.id }))}/><span className="hbdr-protect-wizard-card-copy"><span className="hbdr-protect-wizard-card-heading"><strong>{repo.name}</strong><em>{repo.type}</em></span><small title={`Bucket / path: ${repo.bucket || '—'} · Endpoint: ${repo.endpoint || '—'}`}>Bucket / path: {repo.bucket || '—'} · Endpoint: {repo.endpoint || '—'}</small></span></label>)}{repositories.length === 0 && <p>No repositories match the search.</p>}</div><div className="hbdr-protect-wizard-pagination"><span>Showing {repositories.length} of {props.storage.length} repositories</span></div></section>}
          {props.step === 4 && <section><h3>Protection policy</h3><p>Pick the schedule and retention rule these namespaces will follow. Both can be changed later without re-running the wizard.</p><div className="hbdr-protect-wizard-policy-tools"><label><Search size={17}/><input value={policyQuery} onChange={event => { setPolicyQuery(event.target.value); setPolicyPage(1); }} placeholder="Search policies" /></label><button type="button" onClick={() => openCreate('policy')}><Plus size={15}/>New policy</button></div><div className="hbdr-protect-wizard-options">{pagePolicies.map(policy => <label key={policy.id} className={props.protectConfig.policy === policy.id ? 'is-selected' : ''}><input type="radio" name="protection-policy" checked={props.protectConfig.policy === policy.id} onChange={() => props.setProtectConfig(prev => ({ ...prev, policy: policy.id }))}/><span className="hbdr-protect-wizard-card-copy"><span className="hbdr-protect-wizard-card-heading"><strong>{policy.name}</strong><em>{policy.type}</em></span><small title={`Schedule: ${policy.schedule} · Retention: ${policy.retention} · ${policy.status} · ${policy.bound ? `${policy.bound} applications bound` : 'No applications bound'}`}>Schedule: {policy.schedule} · Retention: {policy.retention} · {policy.status} · {policy.bound ? `${policy.bound} applications bound` : 'No applications bound'}</small></span></label>)}{policies.length === 0 && <p>No policies match the search.</p>}</div><div className="hbdr-protect-wizard-pagination"><span>Showing {policies.length ? (currentPage - 1) * pageSize + 1 : 0}–{Math.min(currentPage * pageSize, policies.length)} of {policies.length}</span><div><button type="button" disabled={currentPage === 1} onClick={() => setPolicyPage(currentPage - 1)} aria-label="Previous policy page"><ChevronLeft size={16}/></button><span>{currentPage} / {pageCount}</span><button type="button" disabled={currentPage === pageCount} onClick={() => setPolicyPage(currentPage + 1)} aria-label="Next policy page"><ChevronRight size={16}/></button></div></div></section>}
          {props.step === 5 && <section><h3>Hooks</h3><p>Pre- and post-operation scripts are optional. This platform version does not support script execution yet.</p><div className="hbdr-protect-wizard-summary">No hooks will be added to this plan.</div></section>}
          {props.step === 6 && <section><h3>Review protection</h3><p>Check the selections before creating the protection plan.</p><dl className="hbdr-protect-wizard-review"><div><dt>Applications</dt><dd>{props.targetNames.join(', ')}</dd></div><div><dt>Target cluster</dt><dd>{selectedTarget?.name || '—'}</dd></div><div><dt>Storage</dt><dd>{selectedRepo?.name || '—'}</dd></div><div><dt>Policy</dt><dd>{selectedPolicy?.name || '—'}</dd></div><div><dt>Hooks</dt><dd>None</dd></div></dl></section>}

        </main>
      </div>
      <footer className="hbdr-protect-wizard-footer"><span>Step {props.step} of 6</span><div><button type="button" onClick={closeWizard}>Cancel</button><button type="button" disabled={props.step === 1} onClick={() => props.setStep((props.step - 1) as ProtectWizardStep)}>Back</button><button type="button" className="is-primary" disabled={!canNext || props.submitting} onClick={goNext}>{props.step === 6 ? props.submitting ? 'Saving…' : 'Create protection' : 'Next'}<ChevronRight size={16}/></button></div></footer>
      {inlineCreate === 'policy' && <ResourceCreateDrawer title="New DR Policy" kind="policy" onClose={() => closeInline()} actions={<><button type="button" disabled={creationBusy || !policyForm.name.trim()} onClick={() => void createResource()}>{creationBusy ? 'Saving...' : 'Save Policy'}</button><button type="button" onClick={() => closeInline()}>Cancel</button></>}><PolicyFormFields policyForm={policyForm} setPolicyForm={setPolicyForm} />{creationError && <p role="alert">{creationError}</p>}</ResourceCreateDrawer>}
      {inlineCreate === 'storage' && storageDraft && <ResourceCreateDrawer title="New Storage Repository" kind="storage" onClose={() => closeInline()} actions={<><button type="button" disabled={creationBusy || !storageReady(storageDraft)} onClick={() => void createResource()}>{creationBusy ? 'Saving...' : 'Save Storage'}</button><button type="button" onClick={() => closeInline()}>Cancel</button></>}><StorageCreateFields draft={storageDraft} setDraft={setStorageDraft} testResult={storageTestResult} testing={creationBusy} onTest={() => void testStorageDraft()} />{creationError && <p role="alert">{creationError}</p>}</ResourceCreateDrawer>}
      {inlineCreate === 'target' && <ResourceCreateDrawer title="Register New Cluster" kind="cluster" onClose={() => closeInline()} actions={registrationMode === 'command' ? <><button type="button" className="is-primary" onClick={() => closeInline(true)}>Continue in Background</button><button type="button" onClick={() => closeInline()}>Cancel</button></> : <button type="button" onClick={() => closeInline()}>Cancel</button>}>
        <ClusterRegistrationChoices registrationType={registrationType} registrationMode={registrationMode === 'platform' ? 'platform-direct' : 'command'} installLoading={creationBusy} onTypeChange={value => { setRegistrationType(value); setKubeconfigUpload(null); setClusterInspection(null); setInstallCommand(''); setCommandCopied(false); setRegistrationWatchStarted(false); }} onModeChange={value => { setCommandCopied(false); setRegistrationWatchStarted(false); setRegistrationMode(value === 'platform-direct' ? 'platform' : 'command'); }} />
        {registrationMode === 'command' ? <ClusterCommandRegistrationFields registrationType={registrationType} prepareNodeCommand={prepareNodeCommand} installCommand={installCommand} installLoading={creationBusy} installError={creationError || null} copied={commandCopied} caCopied={caCommandCopied} registryCACommandRef={registryCACommandRef} installCommandRef={installCommandRef} onCopyCA={() => { void navigator.clipboard.writeText(prepareNodeCommand).then(() => setCaCommandCopied(true)); }} onCopyInstall={() => { void navigator.clipboard.writeText(installCommand).then(() => { setCommandCopied(true); setRegistrationWatchStarted(true); }); }} onRegenerate={() => void createResource()} /> : <ClusterPlatformRegistrationFields registrationType={registrationType} cceUpload={kubeconfigUpload} cceContext={kubeconfigContext} cceUploadLoading={creationBusy} cceUploadError={creationError} cceInspection={clusterInspection} cceInspectionLoading={creationBusy} cceStorageClass={registrationStorageClass} cceRegistrationTask={registrationTask} onUpload={file => void uploadKubeconfig(file)} onContextChange={setKubeconfigContext} onInspect={() => void inspectCluster()} onStorageClassChange={setRegistrationStorageClass} onRegister={() => void registerCluster()} onCancel={() => void cancelRegistration()} />}
      </ResourceCreateDrawer>}

    </div>
  </div>;
}
