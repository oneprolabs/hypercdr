import type { Dispatch, SetStateAction } from 'react';
import { Activity } from 'lucide-react';
import { StorageFormFields } from './storage-form-fields';
import { createStorageDraft, type StorageDraft } from './storage-form-model';

const types = [
  ['S3', 'Amazon S3'], ['S3-Compatible', 'S3 Compatible'],
  ['Azure', 'Azure Blob'], ['Google Cloud', 'Google Cloud'],
] as const;

export function StorageCreateFields({ draft, setDraft, testResult, testing, onTest }: {
  draft: StorageDraft;
  setDraft: Dispatch<SetStateAction<StorageDraft | null>>;
  testResult?: { tone: 'ok' | 'fail'; text: string } | null;
  testing: boolean;
  onTest: () => void;
}) {
  return <>
    <section className="hbdr-advanced-filter-section"><h4>Repository Type</h4><div className="hbdr-advanced-filter-box hbdr-storage-type-select-box"><label><span>Type</span><select value={draft.type} onChange={event => { const next = createStorageDraft(event.target.value); next.name = draft.name; setDraft(next); }}>{types.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></div></section>
    <section className="hbdr-advanced-filter-section"><h4>Configuration</h4><div className="hbdr-advanced-filter-box hbdr-storage-config-box"><StorageFormFields editingRepo={draft} setEditingRepo={setDraft} allowTypeChange /></div>
      <div className="hbdr-storage-connection-check">{testResult && <div className={`hbdr-storage-test-result ${testResult.tone === 'ok' ? 'is-ok' : 'is-fail'}`}>{testResult.text}</div>}<button type="button" onClick={onTest} disabled={testing} className="hbdr-storage-test-button"><Activity size={14}/>{testing ? 'Testing...' : 'Test Connection'}</button></div>
    </section>
  </>;
}
