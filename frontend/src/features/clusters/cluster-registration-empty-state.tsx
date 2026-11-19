import { Plus, Server } from 'lucide-react';

export function ClusterRegistrationEmptyState({ onRegister }: { onRegister: () => void }) {
  return (
    <div className="hbdr-cluster-registration-empty">
      <Server size={36} className="mx-auto mb-3 text-slate-300" />
      <h3 className="text-sm font-semibold text-slate-800">No registered clusters yet</h3>
      <p className="mt-1 text-xs text-slate-500">Register your first Kubernetes cluster. The DR workspace becomes available after the Agent connects.</p>
      <button type="button" onClick={onRegister} className="hbdr-dr-action-primary mt-5 inline-flex items-center gap-1.5"><Plus size={15} />Register Cluster</button>
    </div>
  );
}
