import type { ReactNode } from 'react';
import { X } from 'lucide-react';

/** Shared presentation for create flows opened from management pages or the DR wizard. */
export function ResourceCreateDrawer({ title, kind, onClose, children, actions, className = '' }: {
  title: string;
  kind: 'policy' | 'storage' | 'cluster';
  onClose: () => void;
  children: ReactNode;
  actions: ReactNode;
  className?: string;
}) {
  return <div className="hbdr-resource-create-layer">
    <div className="hbdr-filter-drawer-backdrop" onClick={onClose} />
    <aside className={`hbdr-filter-drawer hbdr-resource-create-drawer hbdr-resource-create-${kind} ${className}`} role="dialog" aria-modal="true" aria-label={title}>
      <div className="hbdr-filter-drawer-head"><strong>{title}</strong><button type="button" onClick={onClose} aria-label={`Close ${title}`}><X size={18} /></button></div>
      <div className="hbdr-filter-drawer-body hbdr-resource-create-body">{children}</div>
      <div className="hbdr-filter-drawer-actions hbdr-resource-create-actions">{actions}</div>
    </aside>
  </div>;
}
