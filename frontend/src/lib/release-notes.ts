import manifest from '../generated/release-notes.json';

export type ReleaseNoteAudience = 'user' | 'admin';
export type ReleaseNoteCategory = 'feature' | 'improvement' | 'fix';
export type ReleaseNoteEntry = { audience: ReleaseNoteAudience; en: string; 'zh-CN': string };
export type ReleaseNotesManifest = {
  version: string;
  releaseDate: string;
  categories: Record<ReleaseNoteCategory, ReleaseNoteEntry[]>;
};

const embeddedManifest = manifest as ReleaseNotesManifest;
export const releaseNotesManifest: ReleaseNotesManifest = {
  ...embeddedManifest,
  version: import.meta.env.VITE_HCDR_RELEASE_VERSION || embeddedManifest.version,
  releaseDate: import.meta.env.VITE_HCDR_RELEASE_DATE || embeddedManifest.releaseDate,
};
export const releaseNoteCategories: ReleaseNoteCategory[] = ['feature', 'improvement', 'fix'];

const storageKey = (isAdmin: boolean) => `hypercdr.releaseNotes.lastViewedVersion.${isAdmin ? 'admin' : 'user'}`;

export function visibleReleaseNotes(isAdmin: boolean) {
  return releaseNoteCategories.map(category => ({
    category,
    entries: releaseNotesManifest.categories[category].filter(entry => entry.audience === 'user' || isAdmin),
  })).filter(group => group.entries.length > 0);
}

export function hasUnreadReleaseNotes(isAdmin: boolean) {
  if (releaseNotesManifest.version === 'dev' || !visibleReleaseNotes(isAdmin).length) return false;
  return window.localStorage.getItem(storageKey(isAdmin)) !== releaseNotesManifest.version;
}

export function markReleaseNotesViewed(isAdmin: boolean) {
  if (releaseNotesManifest.version === 'dev') return;
  window.localStorage.setItem(storageKey(isAdmin), releaseNotesManifest.version);
}
