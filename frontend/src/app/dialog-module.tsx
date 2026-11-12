import { useEffect, useState, type ReactNode } from 'react';
import { ModalFrame } from '../components/modal-frame';
import { warmPageModules } from './module-loader';

type DialogModule = { preload: () => Promise<unknown>; isReady: () => boolean };

export function useDialogPreload(modules: DialogModule[]) {
  useEffect(() => {
    let current = true;
    const timer = window.setTimeout(() => {
      void warmPageModules(modules.map(module => module.preload), () => current);
    }, 250);
    return () => { current = false; window.clearTimeout(timer); };
  }, [modules]);
}

export function preloadDialog(module: DialogModule) {
  void module.preload().catch(() => undefined);
}

export function DialogModuleFrame({ module, title, onClose, children, embedded = false }: {
  module: DialogModule; title: string; onClose: () => void; children: ReactNode; embedded?: boolean;
}) {
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>(() => module.isReady() ? 'ready' : 'loading');
  useEffect(() => {
    let current = true;
    setStatus(module.isReady() ? 'ready' : 'loading');
    const timer = window.setTimeout(() => { if (current) setStatus('error'); }, 15000);
    void module.preload().then(() => {
      if (current) { window.clearTimeout(timer); setStatus('ready'); }
    }, () => {
      if (current) { window.clearTimeout(timer); setStatus('error'); }
    });
    return () => { current = false; window.clearTimeout(timer); };
  }, [module, attempt]);
  if (status === 'ready') return children;
  const content = status === 'loading' ? <p role="status">Loading…</p> : <div role="alert">
      <p>Unable to load this dialog. Check your connection and try again.</p>
      <div className="mt-4 flex gap-3">
        <button type="button" onClick={() => setAttempt(value => value + 1)}>Retry</button>
        <button type="button" onClick={() => window.location.reload()}>Reload page</button>
      </div>
    </div>;
  return embedded ? content : <ModalFrame title={title} onClose={onClose}>{content}</ModalFrame>;
}
