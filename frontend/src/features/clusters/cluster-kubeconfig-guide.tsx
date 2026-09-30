import { X } from 'lucide-react';
import type { RegistrationType } from './cluster-registration-choices';

const titles: Record<RegistrationType, string> = {
  'native-kubernetes': 'Get a Native Kubernetes kubeconfig',
  'huaweicloud-cce': 'Get a Huawei Cloud CCE kubeconfig',
  openshift: 'Get an OpenShift kubeconfig',
};

export function ClusterKubeconfigHelp({ type, mode }: { type: RegistrationType; mode: 'command' | 'platform-direct' }) {
  return <details className="hbdr-register-kubeconfig-help">
    <summary>How do I get {type === 'openshift' ? 'an OpenShift' : type === 'huaweicloud-cce' ? 'a Huawei Cloud CCE' : 'a Native Kubernetes'} kubeconfig?</summary>
    {type === 'native-kubernetes' && <ol>
      <li>On a control-plane or administration host, check <code>kubectl config current-context</code> and <code>kubectl auth can-i create clusterroles.rbac.authorization.k8s.io</code>.</li>
      <li>{mode === 'command' ? <>Run the install command with that user's <code>~/.kube/config</code>, or append <code>--kubeconfig /path/to/config</code>.</> : <>Export the active context with <code>kubectl config view --raw --minify --flatten &gt; hypercdr-native-kubeconfig.yaml</code>, then upload that file here.</>}</li>
    </ol>}
    {type === 'huaweicloud-cce' && <ol>
      <li>In Huawei Cloud CCE, open the target cluster and download its kubeconfig from the cluster connection or kubectl access page using cluster administrator credentials.</li>
      <li>{mode === 'command' ? <>Save it on the command host as <code>~/.kube/hypercdr-cce.yaml</code>, or append <code>--kubeconfig /path/to/config</code>.</> : 'Upload the downloaded YAML file here, then select the target context.'}</li>
    </ol>}
    {type === 'openshift' && <ol>
      <li>In the OpenShift console, choose <strong>Copy login command</strong> from the user menu. Run the complete <code>oc login</code> command on an administration host.</li>
      <li>Check <code>oc auth can-i create clusterroles.rbac.authorization.k8s.io</code> returns <code>yes</code>.</li>
      <li>{mode === 'command' ? <>Run the install command using the active <code>~/.kube/config</code>, or append <code>--kubeconfig /path/to/config</code>.</> : <>Export the active context with <code>oc config view --raw --minify --flatten &gt; hypercdr-openshift-kubeconfig.yaml</code>, then upload that file here.</>}</li>
    </ol>}
  </details>;
}

export function ClusterKubeconfigGuide({ type, onClose }: { type: RegistrationType; onClose: () => void }) {
  return <div className="hbdr-kubeconfig-guide">
    <div className="hbdr-filter-drawer-backdrop" onClick={onClose} />
    <aside className="hbdr-filter-drawer" role="dialog" aria-modal="true" aria-label={titles[type]}>
      <div className="hbdr-filter-drawer-head"><strong>{titles[type]}</strong><button type="button" onClick={onClose} aria-label="Close kubeconfig guide"><X size={18} /></button></div>
      <div className="hbdr-filter-drawer-body hbdr-kubeconfig-guide-body">
        {type === 'native-kubernetes' && <>
          <p>On a control-plane host, use a cluster-admin context that can install the HyperCDR agent.</p>
          <ol><li>Confirm the active context and permissions:<pre>kubectl config current-context{'\n'}kubectl auth can-i create clusterroles.rbac.authorization.k8s.io</pre></li>
          <li>Export only that context to a portable file:<pre>kubectl config view --raw --minify --flatten &gt; hypercdr-native-kubeconfig.yaml{'\n'}chmod 600 hypercdr-native-kubeconfig.yaml</pre></li>
          <li>Copy the file to your workstation and upload it in the registration panel.</li></ol>
        </>}
        {type === 'huaweicloud-cce' && <>
          <p>Use an account with permission to manage the target CCE cluster.</p>
          <ol><li>In the Huawei Cloud console, open <strong>Cloud Container Engine → Clusters</strong> and select the cluster.</li>
          <li>Download its kubeconfig from the cluster connection or kubectl access page. Choose credentials with cluster administrator permissions.</li>
          <li>If the downloaded configuration contains several contexts, keep the target context selected. Upload the YAML file, then confirm the context shown by HyperCDR.</li></ol>
        </>}
        {type === 'openshift' && <>
          <p>Use a Linux administration host with the <code>oc</code> CLI and cluster-admin access.</p>
          <ol><li>In the OpenShift Web Console, open the user menu and choose <strong>Copy login command</strong>. Run the complete <code>oc login</code> command on the administration host.</li>
          <li>Verify the session:<pre>oc whoami{'\n'}oc get nodes{'\n'}oc auth can-i create clusterroles.rbac.authorization.k8s.io</pre>The permission check must return <strong>yes</strong>.</li>
          <li>Export and upload the active context:<pre>oc config view --raw --minify --flatten &gt; hypercdr-openshift-kubeconfig.yaml{'\n'}chmod 600 hypercdr-openshift-kubeconfig.yaml</pre></li></ol>
        </>}
        <p className="hbdr-kubeconfig-guide-security">The file contains administrator credentials. Keep local copies secure and remove them after registration. HyperCDR deletes the uploaded temporary file when registration ends or expires.</p>
      </div>
      <div className="hbdr-filter-drawer-actions"><button type="button" onClick={onClose}>Done</button></div>
    </aside>
  </div>;
}
