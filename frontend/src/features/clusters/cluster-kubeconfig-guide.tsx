import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';

type GuideMode = 'command' | 'platform-direct';

function CommandBlock({ label, command }: { label: string; command: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };
  return <div className="hbdr-kubeconfig-command">
    <div className="hbdr-kubeconfig-command-head">
      <span>{label}</span>
      <button type="button" onClick={() => void copy()} aria-label={`Copy ${label}`}>
        {copied ? <Check size={13} /> : <Copy size={13} />}{copied ? 'Copied' : 'Copy'}
      </button>
    </div>
    <pre>{command}</pre>
  </div>;
}

export function ClusterKubeconfigHelp({ type, mode }: { type: RegistrationType; mode: GuideMode }) {
  const [open, setOpen] = useState(false);
  const title = type === 'openshift' ? 'an OpenShift' : type === 'huaweicloud-cce' ? 'a Huawei Cloud CCE' : 'a Native Kubernetes';
  return <div className="hbdr-register-kubeconfig-help">
    <button type="button" className="hbdr-register-kubeconfig-toggle" aria-expanded={open} onClick={() => setOpen(value => !value)}>
      How do I get {title} kubeconfig?<span aria-hidden="true">{open ? '−' : '+'}</span>
    </button>
    {open && <div className="hbdr-register-kubeconfig-steps">
      {type === 'native-kubernetes' && <>
        <p>On a control-plane or administration host, use a context with cluster-admin permissions.</p>
        <CommandBlock label="Check context and permissions" command={'kubectl config current-context\nkubectl auth can-i create clusterroles.rbac.authorization.k8s.io'} />
        <p>The permission check must print <code>yes</code>.</p>
        {mode === 'platform-direct' ? <>
          <p>Export the active context to a portable file, then upload that file above.</p>
          <CommandBlock label="Export kubeconfig" command={'kubectl config view --raw --minify --flatten > hypercdr-native-kubeconfig.yaml\nchmod 600 hypercdr-native-kubeconfig.yaml'} />
        </> : <>
          <p>Run the installer as the user whose <code>~/.kube/config</code> contains that context. If the file is elsewhere, append this option to the install command:</p>
          <CommandBlock label="Optional kubeconfig argument" command="--kubeconfig /absolute/path/to/kubeconfig" />
        </>}
      </>}
      {type === 'huaweicloud-cce' && <>
        <p>In Huawei Cloud CCE, open <strong>Clusters → target cluster → Cluster Connection / kubectl Access</strong>. Download the kubeconfig with cluster administrator permissions.</p>
        {mode === 'platform-direct' ? <p>Upload the downloaded YAML file above and select its target context.</p> : <>
          <p>Copy the file to the Linux host where you run the installer. Save it as <code>~/.kube/hypercdr-cce.yaml</code>; the installer detects this path. For another path, append:</p>
          <CommandBlock label="Optional kubeconfig argument" command="--kubeconfig /absolute/path/to/cce-kubeconfig.yaml" />
        </>}
      </>}
      {type === 'openshift' && <>
        <p>In the OpenShift Web Console, open the user menu and choose <strong>Copy login command</strong>. Run that command on a Linux administration host with the <code>oc</code> CLI.</p>
        <CommandBlock label="Check login and permissions" command={'oc whoami\noc auth can-i create clusterroles.rbac.authorization.k8s.io'} />
        <p>The permission check must print <code>yes</code>.</p>
        {mode === 'platform-direct' ? <>
          <p>Export the active context to a portable file, then upload that file above.</p>
          <CommandBlock label="Export kubeconfig" command={'oc config view --raw --minify --flatten > hypercdr-openshift-kubeconfig.yaml\nchmod 600 hypercdr-openshift-kubeconfig.yaml'} />
        </> : <>
          <p>Run the installer as the user who ran <code>oc login</code>; it can use that user's <code>~/.kube/config</code>. For another path, append:</p>
          <CommandBlock label="Optional kubeconfig argument" command="--kubeconfig /absolute/path/to/openshift-kubeconfig.yaml" />
        </>}
      </>}
      <p className="hbdr-register-kubeconfig-security">A kubeconfig contains credentials. Keep exported files private and delete temporary copies after registration.</p>
    </div>}
  </div>;
}
