import { useEffect, useId, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';

type ConfirmDialogProps = {
  open: boolean;
  title: string;
  description?: string;
  children?: ReactNode;
  value?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  busy?: boolean;
  tone?: 'default' | 'danger';
  onConfirm: () => void;
  onClose: () => void;
};

export function ConfirmDialog({
  open,
  title,
  description,
  children,
  value,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  busy = false,
  tone = 'default',
  onConfirm,
  onClose,
}: ConfirmDialogProps) {
  const titleId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    cancelRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onClose();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [busy, onClose, open]);

  const confirmClass = tone === 'danger'
    ? 'border-rose-600 bg-rose-600 text-white hover:bg-rose-700'
    : 'border-blue-600 bg-blue-600 text-white hover:bg-blue-700';

  return (
    <AnimatePresence>
      {open && (
        <div className="pointer-events-none fixed inset-0 z-[240] flex items-center justify-center p-4">
          <motion.section
            initial={{ opacity: 0, scale: 0.97, y: 6 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.97, y: 6 }}
            transition={{ duration: 0.16, ease: 'easeOut' }}
            className="pointer-events-auto relative w-full max-w-md rounded-xl border border-slate-200 bg-white shadow-2xl ring-1 ring-slate-900/5"
            role="alertdialog"
            aria-modal="false"
            aria-labelledby={titleId}
          >
            <header className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
              <div>
                <h3 id={titleId} className="text-sm font-black text-slate-900">{title}</h3>
                {description && <p className="mt-1 text-xs text-slate-500">{description}</p>}
              </div>
              <button type="button" onClick={onClose} disabled={busy} className="rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="Close confirmation"><X size={17} /></button>
            </header>
            <div className="px-5 py-4">
              {children && <div className="text-sm leading-6 text-slate-600">{children}</div>}
              {value && <p className="mt-2 break-all rounded-lg bg-slate-50 px-3 py-2 font-mono text-[11px] leading-5 text-slate-700">{value}</p>}
            </div>
            <footer className="flex justify-end gap-2 border-t border-slate-100 px-5 py-3">
              <button ref={cancelRef} type="button" disabled={busy} onClick={onClose} className="hbdr-dr-action-secondary">{cancelLabel}</button>
              <button type="button" disabled={busy} onClick={onConfirm} className={`inline-flex h-8 items-center justify-center rounded border px-4 text-xs font-bold disabled:opacity-50 ${confirmClass}`}>{busy ? 'Processing…' : confirmLabel}</button>
            </footer>
          </motion.section>
        </div>
      )}
    </AnimatePresence>
  );
}
