import { Archive, Check, Plus, RefreshCw, Search } from 'lucide-react';
import PageTitleBar from './page-title-bar';

export function SearchBar({ title, desc, action, onAction }: { title: string; desc: string; action?: string; onAction?: () => void }) {
  const ActionIcon = action?.includes('Refresh') ? RefreshCw : action?.includes('Save') ? Check : action?.includes('Export') ? Archive : Plus;
  return (
    <PageTitleBar>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded border border-blue-100 bg-blue-50 text-blue-600"><Search size={18} /></div>
          <div><h3 className="text-sm font-black text-slate-900">{title}</h3><p className="text-xs font-medium text-slate-400">{desc}</p></div>
        </div>
        <div className="flex gap-2">
          {action && <button onClick={onAction} className="hbdr-page-title-action rounded bg-blue-600 px-4 py-2 text-xs font-black text-white"><ActionIcon size={14} className="mr-1 inline" />{action}</button>}
        </div>
      </div>
    </PageTitleBar>
  );
}
