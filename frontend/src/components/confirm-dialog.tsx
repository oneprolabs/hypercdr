import { useEffect, useId, useRef, type ReactNode } from 'react';
import { AlertTriangle, LoaderCircle } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';

type ConfirmDialogProps = {
  open: boolean; title: string; description?: string; children?: ReactNode; value?: string;
  confirmLabel?: string; cancelLabel?: string; busy?: boolean; tone?: 'default' | 'danger';
  onConfirm: () => void; onClose: () => void;
};

export function ConfirmDialog({ open, title, description, children, value, confirmLabel = 'Confirm', cancelLabel = 'Cancel', busy = false, tone = 'default', onConfirm, onClose }: ConfirmDialogProps) {
  const titleId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const destructive = tone === 'danger';

  useEffect(() => {
    if (!open) return;
    cancelRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => event.key === 'Escape' && !busy && onClose();
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [busy, onClose, open]);

  return (
    <AnimatePresence>
      {open && (
        <div className="pointer-events-none fixed inset-0 z-[240] flex items-center justify-center p-4">
          <motion.section
            initial={{ opacity: 0, scale: 0.96, y: 8 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0.96, y: 8 }}
            transition={{ duration: 0.16, ease: 'easeOut' }}
            className={`pointer-events-auto relative w-full max-w-[410px] overflow-hidden rounded-2xl border bg-white shadow-[0_24px_70px_-14px_rgba(15,23,42,0.48)] ring-1 ${destructive ? 'border-rose-200 ring-rose-900/10' : 'border-blue-200 ring-blue-900/10'}`}
            role="alertdialog" aria-modal="false" aria-labelledby={titleId}
          >
            <div className={`h-1 w-full ${destructive ? 'bg-rose-500' : 'bg-blue-500'}`} />
            <div className={`flex gap-3.5 px-5 pb-4 pt-5 ${destructive ? 'bg-gradient-to-b from-rose-50/75 to-white' : 'bg-gradient-to-b from-blue-50/75 to-white'}`}>
              <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full shadow-sm ring-1 ${destructive ? 'bg-white text-rose-600 ring-rose-200' : 'bg-white text-blue-600 ring-blue-200'}`}>
                <AlertTriangle size={19} strokeWidth={2.2} />
              </span>
              <div className="min-w-0 flex-1 pt-0.5">
                <h3 id={titleId} className="text-[15px] font-extrabold leading-5 text-slate-950">{title}</h3>
                {description && <p className="mt-1 text-xs font-medium leading-5 text-slate-500">{description}</p>}
                {children && <div className="mt-3 text-sm leading-5 text-slate-600">{children}</div>}
                {value && <div className="mt-2.5 truncate rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-[11px] text-slate-700" title={value}>{value}</div>}
              </div>
            </div>
            <div className="flex justify-end gap-2 border-t border-slate-200/80 bg-slate-50 px-5 py-3.5">
              <button ref={cancelRef} type="button" disabled={busy} onClick={onClose} className="inline-flex h-8 items-center justify-center rounded-lg border border-slate-200 bg-white px-3.5 text-xs font-bold text-slate-600 shadow-sm transition hover:border-slate-300 hover:bg-slate-50 disabled:opacity-50">{cancelLabel}</button>
              <button type="button" disabled={busy} onClick={onConfirm} className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3.5 text-xs font-bold text-white shadow-sm transition disabled:opacity-50 ${destructive ? 'bg-rose-600 hover:bg-rose-700' : 'bg-blue-600 hover:bg-blue-700'}`}>
                {busy && <LoaderCircle size={13} className="animate-spin" />}{busy ? 'Processing…' : confirmLabel}
              </button>
            </div>
          </motion.section>
        </div>
      )}
    </AnimatePresence>
  );
}
