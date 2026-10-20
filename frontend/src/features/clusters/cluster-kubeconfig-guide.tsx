import { useEffect, useRef, useState } from 'react';
import { Check, Copy } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';

type GuideMode = 'command' | 'platform-direct';

function CommandBlock({ label, command }: { label: string; command: string }) {
  const [copied, setCopied] = useState(false);
  const commandRef = useRef<HTMLTextAreaElement>(null);
  const resetTimer = useRef<ReturnType<typeof window.setTimeout> | undefined>(undefined);
  useEffect(() => {
    setCopied(false);
    window.clearTimeout(resetTimer.current);
    return () => window.clearTimeout(resetTimer.current);
  }, [command]);
  const copy = async () => {
    let success = false;
    try {
      await navigator.clipboard.writeText(command);
      success = true;
    } catch {
      const field = commandRef.current;
      if (field) {
        field.focus();
        field.select();
        success = document.execCommand('copy');
      }
    }
    window.clearTimeout(resetTimer.current);
    setCopied(success);
    if (success) resetTimer.current = window.setTimeout(() => setCopied(false), 2000);
  };
  return <div className="hbdr-kubeconfig-command">
    <div className="hbdr-kubeconfig-command-head">
      <span>{label}</span>
      <button type="button" disabled={!command} onClick={() => void copy()} aria-label={`Copy ${label}`}>
        {copied ? <Check size={13} /> : <Copy size={13} />}{copied ? 'Copied' : 'Copy'}
      </button>
    </div>
    <pre>{command || 'Generating check command…'}</pre>
    <textarea ref={commandRef} value={command} readOnly className="sr-only" tabIndex={-1} aria-hidden="true" />
  </div>;
}

export function ClusterKubeconfigHelp({ type, mode }: { type: RegistrationType; mode: GuideMode }) {
  if (mode === 'command') return <div className="hbdr-register-kubeconfig-guide">
    <p>Use a <strong>Linux administration host</strong> with <code>bash</code>, <code>curl</code>, and <code>kubectl</code>. It must reach the <strong>target cluster API and HyperCDR server</strong>.</p>
    {type === 'native-kubernetes' && <p>Use the Linux user whose <code>kubectl</code> is configured for the target cluster with <strong>cluster administrator permissions</strong>. A control-plane node is also suitable. No kubeconfig export is needed.</p>}
    {type === 'openshift' && <p>Install <code>oc</code> and complete <code>oc login</code> as a <strong>cluster administrator</strong>. Get the login command from the OpenShift console → <strong>Copy login command</strong>. Use an administration host, not an RHCOS node.</p>}
    {type === 'huaweicloud-cce' && <p>Use a CCE kubeconfig with <strong>cluster administrator permissions</strong>. If not configured, download it from <strong>CCE → target cluster → kubectl Access</strong> and upload it to this host. The installer asks for its path.</p>}
    <p className="hbdr-register-kubeconfig-security">Run the command below as this Linux user; do not switch to a root shell. No separate check command is needed.</p>
  </div>;
  return <div className="hbdr-register-kubeconfig-guide">
    <p>Use a kubeconfig with cluster administrator permissions. HyperCDR must be able to reach its API endpoint. You do not need to run an installer on this host.</p>
    {type === 'native-kubernetes' && <>
      <p>On the Linux host already configured to administer the intended cluster, export its active context.</p>
      <CommandBlock label="Export kubeconfig" command={'kubectl config view --raw --minify --flatten > hypercdr-native-kubeconfig.yaml\nchmod 600 hypercdr-native-kubeconfig.yaml'} />
      <p>Upload <code>hypercdr-native-kubeconfig.yaml</code> below. Inspect the cluster to verify its identity, access, and permissions before registration.</p>
    </>}
    {type === 'huaweicloud-cce' && <p>In <strong>CCE console → target cluster → Cluster Connection / kubectl Access</strong>, download a cluster-admin kubeconfig, then upload the downloaded YAML file below.</p>}
    {type === 'openshift' && <>
      <p>On a Linux administration host with <code>oc</code>, complete <code>oc login</code> using a cluster administrator account. Get the login command from the OpenShift console user menu → <strong>Copy login command</strong>, then export the active context.</p>
      <CommandBlock label="Export kubeconfig" command={'oc config view --raw --minify --flatten > hypercdr-openshift-kubeconfig.yaml\nchmod 600 hypercdr-openshift-kubeconfig.yaml'} />
      <p>Upload <code>hypercdr-openshift-kubeconfig.yaml</code> below. Inspect the cluster before registration.</p>
    </>}
    <p className="hbdr-register-kubeconfig-security">Kubeconfig files contain administrator credentials. Keep them private and remove copies you no longer need.</p>
  </div>;
}
