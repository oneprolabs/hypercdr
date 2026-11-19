import React, { useEffect, useRef, useState } from 'react';
import { ClusterRegistrationChoices } from './cluster-registration-choices';
import { ResourceCreateDrawer } from '../../components/resource-create-drawer';
import { ClusterPlatformRegistrationFields } from './cluster-platform-registration-fields';
import { ClusterCommandRegistrationFields } from './cluster-command-registration-fields';
import { uploadRegistrationKubeconfig, inspectRegistrationCluster, startRegistrationTask, cancelRegistrationTask } from './cluster-registration-service';
import { ApiRequestError, apiGet, apiDelete } from '../../api/client';
import type { Cluster } from './types';
const copyTextToClipboard=async(text:string,textarea?:HTMLTextAreaElement|null)=>{try{await navigator.clipboard.writeText(text);return true}catch{if(!textarea)return false;textarea.focus();textarea.select();return document.execCommand('copy')}};

type ClusterRegistrationType='native-kubernetes'|'huaweicloud-cce'|'openshift';
type CCERegistrationMode='platform-direct'|'command';
type CCEKubeconfigContext={name:string;cluster:string;user:string;apiServer:string;isCurrent:boolean};
type CCEKubeconfigUpload={id:string;fingerprint:string;currentContext:string;contexts:CCEKubeconfigContext[];expiresAt:string};
type CCEInspectionGate={id:string;label:string;status:'passed'|'warning'|'deferred';detail:string};
type CCEInspection={context:string;clusterName:string;clusterId:string;region?:string;serverVersion:string;nodeCount:number;storageClasses:string[];defaultStorageClass?:string;gates:CCEInspectionGate[]};
type ApiAgentToken={installCommand:string;prepareNodeCommand?:string;clusterType?:ClusterRegistrationType};
type ApiTask={id:string;clusterId:string;type:string;status:string;progress:number;errorCode?:string;errorMessage?:string;payload?:Record<string,any>;createdAt?:string;completedAt?:string};

export default function ClusterRegistrationDrawer({ clusters, onClose, onRefreshRegistration, setSelectedCluster, getAgentTokenForRegistration, prefetchAgentToken, toast }: {
  clusters: Cluster[];
  onClose: () => void;
  onRefreshRegistration: () => Promise<Cluster[]>;
  setSelectedCluster: (cluster: Cluster) => void;
  getAgentTokenForRegistration: (clusterType?: ClusterRegistrationType) => Promise<ApiAgentToken>;
  prefetchAgentToken: () => Promise<ApiAgentToken | null> | null;
  toast: (message: string) => void;
}) {
  const [registrationType, setRegistrationType] = useState<ClusterRegistrationType>('native-kubernetes');
  const [cceRegistrationMode, setCCERegistrationMode] = useState<CCERegistrationMode>('command');
  const [cceUpload, setCCEUpload] = useState<CCEKubeconfigUpload | null>(null);
  const [cceContext, setCCEContext] = useState('');
  const [cceUploadLoading, setCCEUploadLoading] = useState(false);
  const [cceUploadError, setCCEUploadError] = useState('');
  const [cceInspection, setCCEInspection] = useState<CCEInspection | null>(null);
  const [cceInspectionLoading, setCCEInspectionLoading] = useState(false);
  const [cceStorageClass, setCCEStorageClass] = useState('');
  const [cceRegistrationTask, setCCERegistrationTask] = useState<ApiTask | null>(null);
  const [cceIdempotencyKey, setCCEIdempotencyKey] = useState('');
  const [registerStep, setRegisterStep] = useState<1 | 2 | 3>(1);
  const [copied, setCopied] = useState(false);
  const [caCopied, setCaCopied] = useState(false);
  const [prepareNodeCommand, setPrepareNodeCommand] = useState('');
  const [installCommand, setInstallCommand] = useState('');
  const [installLoading, setInstallLoading] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [registrationBaseline, setRegistrationBaseline] = useState<string[]>([]);
  const registryCACommandRef = useRef<HTMLTextAreaElement | null>(null);
  const installCommandRef = useRef<HTMLTextAreaElement | null>(null);
  const loadRegistrationCommand = async (clusterType: ClusterRegistrationType) => {
    setRegistrationType(clusterType);
    setRegisterStep(1);
    setCopied(false);
    setInstallLoading(true);
    setInstallError(null);
    setInstallCommand('');
    setPrepareNodeCommand('');
    try {
      const token = await getAgentTokenForRegistration(clusterType);
      setPrepareNodeCommand(token.prepareNodeCommand || '');
      setInstallCommand(token.installCommand);
      if (clusterType === 'native-kubernetes') void prefetchAgentToken();
    } catch (error) {
      setInstallCommand('');
      setPrepareNodeCommand('');
      const message = error instanceof ApiRequestError && error.code === 'LICENSE_NOT_ACTIVE'
        ? 'License required: start a trial or import a formal license in Settings > License Management before registering a cluster.'
        : error instanceof Error ? error.message : 'Install command generation failed.';
      setInstallError(message);
      toast(message);
    } finally { setInstallLoading(false); }
  };

  const resetDirectRegistration = () => {
    if (cceUpload?.id) void apiDelete(`/api/v1/cluster-registrations/kubeconfigs/${encodeURIComponent(cceUpload.id)}`).catch(() => {});
    setCCEUpload(null);
    setCCEContext('');
    setCCEUploadError('');
    setCCEInspection(null);
    setCCEStorageClass('');
    setCCERegistrationTask(null);
    setCCEIdempotencyKey('');
  };

  const closeRegister = () => {
	if (cceRegistrationTask && ['queued', 'running', 'accepted', 'dispatched', 'canceling'].includes(cceRegistrationTask.status)) {
	  toast('Registration is still running. Cancel it explicitly before closing this panel.');
	  return;
	}
    onClose();
    resetDirectRegistration();
  };

  const inspectCCECluster = async () => {
    if (!cceUpload || !cceContext) return;
    setCCEInspectionLoading(true);
    setCCEUploadError('');
    try {
      const inspection = await inspectRegistrationCluster<CCEInspection>(cceUpload.id, cceContext, registrationType);
      setCCEInspection(inspection);
      setCCEStorageClass(inspection.defaultStorageClass || (inspection.storageClasses.length === 1 ? inspection.storageClasses[0] : ''));
    } catch (error) {
      setCCEInspection(null);
      setCCEUploadError(error instanceof Error ? error.message : 'Cluster inspection failed.');
    } finally { setCCEInspectionLoading(false); }
  };

  const startCCEDirectRegistration = async () => {
    if (!cceUpload || !cceContext || !cceInspection) return;
    setCCEUploadError('');
    setCCEInspectionLoading(true);
    try {
      const key = cceIdempotencyKey || `cce-${crypto.randomUUID()}`;
      setCCEIdempotencyKey(key);
      setCCERegistrationTask(await startRegistrationTask<ApiTask>(cceUpload.id, cceContext, cceStorageClass, registrationType, key));
    } catch (error) {
      setCCEUploadError(error instanceof Error ? error.message : 'Cluster registration could not be started.');
    } finally { setCCEInspectionLoading(false); }
  };

  const cancelCCEDirectRegistration = async () => {
    if (!cceRegistrationTask) return;
    setCCEInspectionLoading(true);
    try {
      setCCERegistrationTask(await cancelRegistrationTask<ApiTask>(cceRegistrationTask.id));
    } catch (error) {
      setCCEUploadError(error instanceof Error ? error.message : 'Registration cancellation failed.');
    } finally { setCCEInspectionLoading(false); }
  };

  useEffect(() => {
    const task = cceRegistrationTask;
    if (!task || !['queued', 'running', 'accepted', 'dispatched'].includes(task.status)) return;
    const timer = window.setInterval(() => {
      void apiGet<ApiTask>(`/api/v1/tasks/${encodeURIComponent(task.id)}`).then(updated => setCCERegistrationTask(updated)).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [cceRegistrationTask?.id, cceRegistrationTask?.status]);

  useEffect(() => {
    if (!cceRegistrationTask || cceRegistrationTask.status !== 'succeeded') return;
    let cancelled = false;
    void onRefreshRegistration().then(nextClusters => {
      if (cancelled) return;
      const latest = nextClusters.find(cluster => cluster.id === cceRegistrationTask.clusterId)
        || nextClusters.find(cluster => !registrationBaseline.includes(cluster.id));
      if (latest && registrationBaseline.length === 0) setSelectedCluster(latest);
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [cceRegistrationTask?.id, cceRegistrationTask?.status, cceRegistrationTask?.clusterId, onRefreshRegistration, registrationBaseline, setSelectedCluster]);

  const uploadCCEKubeconfig = async (file: File | undefined) => {
    if (!file) return;
    setCCEUploadLoading(true);
    setCCEUploadError('');
    setCCEInspection(null);
    setCCEStorageClass('');
    setCCERegistrationTask(null);
    setCCEIdempotencyKey('');
    try {
      const upload = await uploadRegistrationKubeconfig<CCEKubeconfigUpload>(file, registrationType);
      if (cceUpload?.id && cceUpload.id !== upload.id) void apiDelete(`/api/v1/cluster-registrations/kubeconfigs/${encodeURIComponent(cceUpload.id)}`).catch(() => {});
      setCCEUpload(upload);
      const current = upload.contexts.find(context => context.isCurrent)?.name || (upload.contexts.length === 1 ? upload.contexts[0].name : '');
      setCCEContext(current);
    } catch (error) {
      setCCEUpload(null);
      setCCEContext('');
      setCCEUploadError(error instanceof Error ? error.message : 'Kubeconfig validation failed.');
    } finally {
      setCCEUploadLoading(false);
    }
  };

  const copyInstallCommand = async (command: string) => {
    if (await copyTextToClipboard(command, installCommandRef.current)) {
      setCopied(true);
      setRegisterStep(3);
      toast('Install command copied');
      window.setTimeout(() => setCopied(false), 1800);
    } else {
      installCommandRef.current?.focus();
      installCommandRef.current?.select();
      toast('Clipboard is unavailable. The install command is selected; press Ctrl+C to copy it.');
    }
  };

  const copyRegistryCACommand = async () => {
    if (await copyTextToClipboard(prepareNodeCommand, registryCACommandRef.current)) {
      setCaCopied(true);
      toast('Node prepare command copied');
      window.setTimeout(() => setCaCopied(false), 1800);
    } else {
      registryCACommandRef.current?.focus();
      registryCACommandRef.current?.select();
      toast('Clipboard is unavailable. The node prepare command is selected; press Ctrl+C to copy it.');
    }
  };

  const finishRegisterCluster = () => {
    toast('Waiting for the agent to connect. The cluster card appears after registration succeeds.');
    closeRegister();
  };

  useEffect(() => {
    if (registerStep !== 3) return;
    let cancelled = false;
    const poll = async () => {
      try {
        const nextClusters = await onRefreshRegistration();
        if (cancelled) return;
        const latest = nextClusters.find(cluster => !registrationBaseline.includes(cluster.id));
        if (latest) {
          if (registrationBaseline.length === 0) setSelectedCluster(latest);
          toast(`${latest?.name || 'Cluster'} registered and connected`);
          closeRegister();
        }
      } catch {
        if (!cancelled) setInstallError('Waiting for agent connection. The platform API is temporarily unreachable.');
      }
    };
    poll();
    const timer = window.setInterval(poll, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [registerStep, registrationBaseline, onRefreshRegistration, setSelectedCluster, toast]);


  const initialized = useRef(false);
  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    setRegistrationBaseline(clusters.map(cluster => cluster.id));
    void loadRegistrationCommand('native-kubernetes');
  }, []);

  return (
      <ResourceCreateDrawer title="Register New Cluster" kind="cluster" onClose={closeRegister} actions={cceRegistrationMode === 'command' ? <><button type="button" className="is-primary" onClick={finishRegisterCluster}>Continue in Background</button><button type="button" onClick={closeRegister}>Cancel</button></> : <button type="button" onClick={closeRegister}>{cceRegistrationTask && ['succeeded', 'failed', 'canceled'].includes(cceRegistrationTask.status) ? 'Done' : 'Cancel'}</button>}>
        <ClusterRegistrationChoices registrationType={registrationType} registrationMode={cceRegistrationMode} installLoading={installLoading} onTypeChange={value => { resetDirectRegistration(); void loadRegistrationCommand(value); }} onModeChange={value => { setRegisterStep(1); setCopied(false); setCCERegistrationMode(value); }} />
        {cceRegistrationMode === 'platform-direct' && <ClusterPlatformRegistrationFields registrationType={registrationType} cceUpload={cceUpload} cceContext={cceContext} cceUploadLoading={cceUploadLoading} cceUploadError={cceUploadError} cceInspection={cceInspection} cceInspectionLoading={cceInspectionLoading} cceStorageClass={cceStorageClass} cceRegistrationTask={cceRegistrationTask} onUpload={file => void uploadCCEKubeconfig(file)} onContextChange={setCCEContext} onInspect={() => void inspectCCECluster()} onStorageClassChange={setCCEStorageClass} onRegister={() => void startCCEDirectRegistration()} onCancel={() => void cancelCCEDirectRegistration()} />}
        {cceRegistrationMode === 'command' && <ClusterCommandRegistrationFields registrationType={registrationType} prepareNodeCommand={prepareNodeCommand} installCommand={installCommand} installLoading={installLoading} installError={installError} copied={copied} caCopied={caCopied} registryCACommandRef={registryCACommandRef} installCommandRef={installCommandRef} onCopyCA={() => void copyRegistryCACommand()} onCopyInstall={command => void copyInstallCommand(command)} onRegenerate={() => void loadRegistrationCommand(registrationType)} />}
      </ResourceCreateDrawer>
  );
}
