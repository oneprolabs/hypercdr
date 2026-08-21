import type { ReactNode } from 'react';
import { X } from 'lucide-react';
import { motion } from 'motion/react';

export function ModalFrame({ title, subtitle, icon, children, onClose, maxWidthClass = 'max-w-2xl' }: { title:string; subtitle?:string; icon?:ReactNode; children:ReactNode; onClose:()=>void; maxWidthClass?:string }) {
  const titleId = `hbdr-drawer-${title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') || 'dialog'}`;
  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={onClose} />
      <motion.aside
        initial={{ opacity: 0, x: 32 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 32 }}
        transition={{ duration: 0.18, ease: 'easeOut' }}
        className={`hbdr-filter-drawer ${maxWidthClass}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="hbdr-filter-drawer-head">
          <div className="flex items-start gap-3">
            {icon && <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-blue-600 text-white shadow-sm">{icon}</div>}
            <div>
              <h3 id={titleId} className="text-lg font-black leading-tight text-slate-950">{title}</h3>
              {subtitle && <p className="mt-1 text-xs font-semibold leading-5 text-slate-500">{subtitle}</p>}
            </div>
          </div>
          <button type="button" onClick={onClose} aria-label={`Close ${title}`}><X size={18} /></button>
        </div>
        <div className="hbdr-filter-drawer-body">{children}</div>
      </motion.aside>
    </div>
  );
}
