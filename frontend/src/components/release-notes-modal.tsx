import React from 'react';
import { CheckCircle2, Sparkles, Wrench, X, Zap } from 'lucide-react';
import { releaseNotesManifest, visibleReleaseNotes, type ReleaseNoteCategory } from '../lib/release-notes';

const categoryMeta: Record<ReleaseNoteCategory, { label: string; icon: React.ElementType }> = {
  feature: { label: 'Features', icon: Sparkles },
  improvement: { label: 'Improvements', icon: Zap },
  fix: { label: 'Fixes', icon: Wrench },
};

export function ReleaseNotesModal({ open, isAdmin, onClose }: { open: boolean; isAdmin: boolean; onClose: () => void }) {
  if (!open) return null;
  const groups = visibleReleaseNotes(isAdmin);
  return <div className="hbdr-release-notes-layer" role="presentation">
    <button type="button" className="hbdr-release-notes-backdrop" aria-label="Close release notes" onClick={onClose} />
    <section className="hbdr-release-notes-modal" role="dialog" aria-modal="true" aria-labelledby="hbdr-release-notes-title">
      <header>
        <div className="hbdr-release-notes-mark"><Sparkles size={18} /></div>
        <div><span>WHAT'S NEW</span><h2 id="hbdr-release-notes-title">Release notes</h2></div>
        <button type="button" onClick={onClose} aria-label="Close"><X size={17} /></button>
      </header>
      <div className="hbdr-release-notes-version">
        <div><small>Current version</small><strong>HyperCDR {releaseNotesManifest.version}</strong></div>
        <div><small>Release date</small><strong>{releaseNotesManifest.releaseDate || 'Development build'}</strong></div>
      </div>
      <div className="hbdr-release-notes-content">
        {groups.map(group => {
          const MetaIcon = categoryMeta[group.category].icon;
          return <section key={group.category} className={`is-${group.category}`}>
            <div className="hbdr-release-notes-category"><span><MetaIcon size={13} />{categoryMeta[group.category].label}</span><em>{group.entries.length}</em></div>
            <ul>{group.entries.map((entry, index) => <li key={`${group.category}-${index}`}>
              <CheckCircle2 size={14} />
              <div>{entry.audience === 'admin' && <small>ADMIN</small>}<span>{entry.en}</span></div>
            </li>)}</ul>
          </section>;
        })}
        {!groups.length && <div className="hbdr-release-notes-empty">No user-facing changes are listed for this build.</div>}
      </div>
    </section>
  </div>;
}
