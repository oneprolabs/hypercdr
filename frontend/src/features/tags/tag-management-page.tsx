import React, { useMemo, useState } from 'react';
import { Archive, ChevronDown, Edit2, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiPatch, apiPost } from '../../api/client';
import { EditField } from '../../components/edit-field';
import { ModalFrame } from '../../components/modal-frame';
import { HyperTable, type HyperTableColumn } from '../../components/table';

type TagItem = { id:string; name:string; createdAt:string };
type TagCluster = { name:string; apps:Array<{name:string; tags?:string[]}> };

function Info({ label, value }: { label:string; value:string }) {
  return <div className="rounded bg-slate-50 p-3"><p className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</p><p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p></div>;
}

export default function TagManagementPage<T extends TagCluster>({
  tags,
  setTags,
  clusters,
  setClusters,
  toast,
}: {
  tags: TagItem[];
  setTags: React.Dispatch<React.SetStateAction<TagItem[]>>;
  clusters: T[];
  setClusters: React.Dispatch<React.SetStateAction<T[]>>;
  toast: (msg: string) => void;
}) {
  const [query, setQuery] = useState('');
  const [editingTag, setEditingTag] = useState<TagItem | null>(null);
  const [draftName, setDraftName] = useState('');
  const [tagModalOpen, setTagModalOpen] = useState(false);
  const [deleteTags, setDeleteTags] = useState<TagItem[]>([]);
  const [selectedTagIds, setSelectedTagIds] = useState<string[]>([]);
  const [tagBulkMenuOpen, setTagBulkMenuOpen] = useState(false);

  const tagResources = (tagId: string) => clusters.flatMap(cluster =>
    cluster.apps
      .filter(app => (app.tags || []).includes(tagId))
      .map(app => `${cluster.name} / ${app.name}`)
  );
  const filteredTags = tags.filter(tag => {
    const keyword = query.trim().toLowerCase();
    if (!keyword) return true;
    return [tag.name, tag.createdAt, tagResources(tag.id).join(' ')].some(value => value.toLowerCase().includes(keyword));
  });
  const selectedTags = tags.filter(tag => selectedTagIds.includes(tag.id));
  const singleSelectedTag = selectedTags.length === 1 ? selectedTags[0] : null;
  const allVisibleTagsSelected = filteredTags.length > 0 && filteredTags.every(tag => selectedTagIds.includes(tag.id));
  const toggleVisibleTags = () => {
    setSelectedTagIds(prev => {
      const visibleIds = filteredTags.map(tag => tag.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };
  const toggleSelectedTag = (tagId: string) => {
    setSelectedTagIds(prev => prev.includes(tagId) ? prev.filter(id => id !== tagId) : [...prev, tagId]);
  };
  const tagTableColumns = useMemo<HyperTableColumn<TagItem>[]>(() => [
    {
      id: 'select',
      header: () => (
        <input
          type="checkbox"
          checked={allVisibleTagsSelected}
          onClick={event => event.stopPropagation()}
          onChange={toggleVisibleTags}
        />
      ),
      cell: info => (
        <input
          type="checkbox"
          checked={selectedTagIds.includes(info.row.original.id)}
          onClick={event => event.stopPropagation()}
          onChange={() => toggleSelectedTag(info.row.original.id)}
        />
      ),
      size: 42,
      minSize: 42,
      maxSize: 54,
      enableSorting: false,
      enableResizing: false,
      meta: { align: 'center' },
    },
    {
      id: 'name',
      header: 'Name',
      accessorFn: tag => tag.name,
      size: 260,
      minSize: 180,
      maxSize: 420,
      cell: info => <span className="hbdr-tag-name"><Archive size={15} />{info.row.original.name}</span>,
      meta: { title: tag => tag.name },
    },
    {
      id: 'resources',
      header: 'Attach Resources',
      accessorFn: tag => tagResources(tag.id).join(', '),
      size: 390,
      minSize: 240,
      maxSize: 620,
      cell: info => {
        const resources = tagResources(info.row.original.id);
        return (
          <span className="hbdr-tag-resources">
            {resources.length > 0 ? resources.slice(0, 2).join(', ') : 'Not attached'}
            {resources.length > 2 ? ` +${resources.length - 2}` : ''}
          </span>
        );
      },
      meta: { title: tag => {
        const resources = tagResources(tag.id);
        return resources.length > 0 ? resources.join(', ') : 'Not attached';
      } },
    },
    {
      id: 'createdAt',
      header: 'Create Time',
      accessorFn: tag => tag.createdAt,
      size: 180,
      minSize: 150,
      maxSize: 260,
      cell: info => info.row.original.createdAt,
      meta: { title: tag => tag.createdAt },
    },
  ], [allVisibleTagsSelected, selectedTagIds, clusters]);
  const openCreateTag = () => {
    setEditingTag(null);
    setDraftName('');
    setTagModalOpen(true);
  };
  const openEditTag = (tag: TagItem) => {
    setEditingTag(tag);
    setDraftName(tag.name);
    setTagModalOpen(true);
  };
  const closeTagModal = () => {
    setTagModalOpen(false);
    setEditingTag(null);
    setDraftName('');
  };
  const saveTag = async () => {
    const normalizedName = draftName.trim();
    if (!normalizedName) {
      toast('Enter a tag name');
      return;
    }
    const duplicate = tags.some(tag => tag.id !== editingTag?.id && tag.name.toLowerCase() === normalizedName.toLowerCase());
    if (duplicate) {
      toast('Tag name already exists');
      return;
    }
    try {
      const saved = editingTag ? await apiPatch<TagItem>(`/api/v1/tags/${editingTag.id}`, { name: normalizedName }) : await apiPost<TagItem>('/api/v1/tags', { name: normalizedName });
      setTags(prev => editingTag ? prev.map(tag => tag.id === saved.id ? saved : tag) : [saved, ...prev]);
      toast(editingTag ? 'Tag updated' : 'Tag created'); closeTagModal();
    } catch (error) { toast(error instanceof Error ? error.message : 'Failed to save tag'); }
  };
  const confirmDeleteTags = async () => {
    if (deleteTags.length === 0) return;
    const deleteIds = deleteTags.map(tag => tag.id);
    try { await Promise.all(deleteIds.map(id => apiDelete(`/api/v1/tags/${id}`))); } catch(error) { toast(error instanceof Error ? error.message : 'Failed to delete tags'); return; }
    setTags(prev => prev.filter(tag => !deleteIds.includes(tag.id)));
    setClusters(prev => prev.map(cluster => ({
      ...cluster,
      apps: cluster.apps.map(app => ({ ...app, tags: (app.tags || []).filter(tagId => !deleteIds.includes(tagId)) })),
    })));
    setSelectedTagIds(prev => prev.filter(tagId => !deleteIds.includes(tagId)));
    toast(`${deleteTags.length} tag${deleteTags.length === 1 ? '' : 's'} deleted and detached from resources`);
    setDeleteTags([]);
  };

  return (
    <motion.div key="tags" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Archive size={18} /></div>
            <div>
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Tag Management</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Create tags and attach them to application DR resources.</p>
            </div>
          </div>
          <button type="button" className="hbdr-dr-action-primary" onClick={openCreateTag}>New Tag</button>
        </div>
      </div>

      <div className="hbdr-dr-table-card hbdr-tag-table-list">
        <div className="hbdr-dr-table-head">
          <div className="hbdr-dr-toolbar">
            <div className="hbdr-dr-action-group">
              <button type="button" className="hbdr-dr-action-primary" onClick={openCreateTag}>New Tag</button>
              <div className="relative">
                <button type="button" disabled={selectedTags.length === 0} onClick={() => setTagBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={tagBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {tagBulkMenuOpen && selectedTags.length > 0 && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setTagBulkMenuOpen(false)} />
                      <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-44 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                        <button disabled={!singleSelectedTag} onClick={() => { if (!singleSelectedTag) return; openEditTag(singleSelectedTag); setTagBulkMenuOpen(false); }} className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300">
                          <span className="flex items-center gap-2"><Edit2 size={14} />Edit Tag</span>
                          {!singleSelectedTag && <em className="rounded bg-slate-100 px-1 py-0.5 text-[9px] not-italic text-slate-400">Single</em>}
                        </button>
                        <button onClick={() => { setDeleteTags(selectedTags); setTagBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete Tag</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <label className="hbdr-dr-search hbdr-tag-quick-search"><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Quick search" /></label>
          </div>
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={tagTableColumns}
          data={filteredTags}
          getRowId={row => row.id}
          onRowClick={row => toggleSelectedTag(row.id)}
          getRowClassName={row => selectedTagIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedTagIds.length}
          emptyMessage={query ? 'No tags match the current search.' : 'No tags have been created.'}
          className="hbdr-tag-hyper-table"
        />
      </div>

      <AnimatePresence>
        {tagModalOpen && (
          <ModalFrame title={editingTag ? 'Edit Tag' : 'New Tag'} onClose={closeTagModal}>
            <div className="space-y-5">
              <EditField label="Name" value={draftName} placeholder="critical" onChange={setDraftName} />
              <div className="flex justify-end gap-3">
                <button type="button" onClick={closeTagModal} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button type="button" onClick={saveTag} className="rounded-xl bg-blue-600 px-6 py-2 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700">Save Tag</button>
              </div>
            </div>
          </ModalFrame>
        )}
        {deleteTags.length > 0 && (
          <ModalFrame title="Delete Tag" onClose={() => setDeleteTags([])}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Delete {deleteTags.length} selected tag{deleteTags.length === 1 ? '' : 's'}? They will be detached from all application resources.
              </div>
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Name" value={deleteTags.map(tag => tag.name).join(', ')} />
                <Info label="Attach Resources" value={String(deleteTags.reduce((total, tag) => total + tagResources(tag.id).length, 0))} />
                <Info label="Create Time" value={deleteTags.length === 1 ? deleteTags[0].createdAt : 'Multiple'} />
              </div>
              <div className="flex justify-end gap-3">
                <button type="button" onClick={() => setDeleteTags([])} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button type="button" onClick={confirmDeleteTags} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700">Confirm Delete</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
