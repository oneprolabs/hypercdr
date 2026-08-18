import React, { useEffect, useMemo, useState } from 'react';
import { Activity, Archive, ChevronDown, Cloud, Database, Eye, Grid3X3, Lock, MoreVertical, Settings, ShieldCheck, Trash2, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { apiDelete, apiPatch, apiPost } from '../../api/client';
import { EditField } from '../../components/edit-field';
import ListToolbarControls, { listToolbarQueryFields, matchesColumnFilterToken, parseColumnFilterToken } from '../../components/list-toolbar-controls';
import { ModalFrame } from '../../components/modal-frame';
import { HyperTable, type HyperTableColumn } from '../../components/table';
import { formatDateTime } from '../../lib/date-time';

type Cluster={id:string;name:string};
type StorageRepo={id:string;name:string;type:string;endpoint:string;bucket:string;region:string;useTls:boolean;status:'connected'|'warning'|'unknown';updatedAt:string;lastValidatedAt?:string;config?:Record<string,string|boolean>;urlStyle?:string};
type ApiStorageRepo={id:string;name:string;type:string;endpoint?:string;bucket?:string;region?:string;tlsEnabled:boolean;status:string;updatedAt?:string;createdAt?:string;lastValidatedAt?:string;config?:Record<string,unknown>};
type ApiTask={id:string;status:string;progress:number};
type StorageRepositoryInput={name:string;type:string;endpoint:string;bucket:string;region:string;tlsEnabled:boolean;config:Record<string,string|boolean>;accessKey?:string;secretKey?:string;accountName?:string;accountKey?:string;serviceAccountKey?:string};
const isS3CompatibleType=(type:string)=>['s3-compatible','s3 compatible'].includes(type.toLowerCase());
const buildStorageRepositoryInput=(repo:StorageRepo):StorageRepositoryInput=>{const config=repo.config||{};const compatible=isS3CompatibleType(repo.type);const azure=repo.type==='Azure';const gcs=repo.type==='Google Cloud'||repo.type==='GCS';const domain=String(config.blobDomain||'blob.core.windows.net').replace(/^https?:\/\//,'');const endpoint=String(azure?`${String(config.accountName||'')}.${domain}`:config.endpoint||repo.endpoint||'');const bucket=String(azure?config.container||repo.bucket||'':config.bucket||repo.bucket||'');const rawRegion=String(config.region||repo.region||'').trim();const region=['n/a','na','-'].includes(rawRegion.toLowerCase())?'':rawRegion;const payloadConfig:Record<string,string|boolean>={};if(config.urlStyle)payloadConfig.urlStyle=String(config.urlStyle);if(config.prefix)payloadConfig.prefix=String(config.prefix);if(azure&&config.accountName)payloadConfig.storageAccount=String(config.accountName);return{name:repo.name,type:repo.type,endpoint,bucket,region,tlsEnabled:Boolean(config.useSsl??repo.useTls),config:payloadConfig,accessKey:String(config.accessKey||''),secretKey:String(config.secretKey||''),accountName:azure?String(config.accountName||''):undefined,accountKey:azure?String(config.accountKey||''):undefined,serviceAccountKey:gcs?String(config.serviceAccountKey||''):undefined}};
const mapStorageRepo=(repo:ApiStorageRepo):StorageRepo=>{const raw=(repo.status||'').toLowerCase();const status:StorageRepo['status']=['connected','ready','active'].includes(raw)?'connected':raw==='warning'?'warning':'unknown';const cfg=(repo.config||{}) as Record<string,unknown>;const lastValidatedAt=repo.lastValidatedAt&&new Date(repo.lastValidatedAt).getUTCFullYear()>1?repo.lastValidatedAt:undefined;return{id:repo.id,name:repo.name,type:repo.type||'S3',endpoint:repo.endpoint||'',bucket:repo.bucket||'',region:repo.region||'',useTls:repo.tlsEnabled,status,updatedAt:repo.updatedAt||repo.createdAt||'',lastValidatedAt,urlStyle:typeof cfg.urlStyle==='string'?cfg.urlStyle:'path'}};
function Info({label,value}:{label:string;value:string}){return <div className="rounded bg-slate-50 p-3"><p className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</p><p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p></div>}

export default function StoragePage({ storage, clusters, onStorageCreated }: { storage: StorageRepo[]; clusters: Cluster[]; onStorageCreated?: (repo: StorageRepo) => void }) {
	const [repos, setRepos] = useState(storage.map(repo => normalizeStorageRepo(repo)));
  const [query, setQuery] = useState('');
  const [queryField, setQueryField] = useState('name');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeFilters, setActiveFilters] = useState<string[]>([]);
  const [visibleColumns, setVisibleColumns] = useState(['type', 'bucket', 'region', 'endpoint', 'tls', 'urlStyle', 'status', 'lastValidatedAt']);
  const [selectedRepoIds, setSelectedRepoIds] = useState<string[]>([]);
  const [storageBulkMenuOpen, setStorageBulkMenuOpen] = useState(false);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [detailRepo, setDetailRepo] = useState<StorageRepo | null>(null);
  const [editingRepo, setEditingRepo] = useState<StorageRepo | null>(null);
	const [deleteRepo, setDeleteRepo] = useState<StorageRepo | null>(null);
	const [storageTypeOpen, setStorageTypeOpen] = useState(false);
	const [savingStorage, setSavingStorage] = useState(false);
	const [syncingStorage, setSyncingStorage] = useState(false);
	const [storageError, setStorageError] = useState<string | null>(null);
	const [storageTestMessage, setStorageTestMessage] = useState<{ tone: "ok" | "fail"; text: string } | null>(null);

	useEffect(() => {
		setRepos(storage.map(repo => normalizeStorageRepo(repo)));
	}, [storage]);

  useEffect(() => {
    if (!menuId) return;
    const closeMenu = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest('[data-policy-menu-root]')) return;
      setMenuId(null);
    };
    window.addEventListener('click', closeMenu, true);
    return () => window.removeEventListener('click', closeMenu, true);
  }, [menuId]);

	function normalizeStorageRepo(repo: StorageRepo): StorageRepo {
		if (repo.config) return repo;
		if (repo.type === 'S3') return { ...repo, config: { bucket: repo.bucket, region: repo.region, accessKey: '', secretKey: '' } };
		if (isS3CompatibleType(repo.type)) return { ...repo, type: 'S3-Compatible', config: { bucket: repo.bucket, region: repo.region, endpoint: repo.endpoint, accessKey: '', secretKey: '', useSsl: repo.useTls, urlStyle: 'path' } };
    if (repo.type === 'Azure') return { ...repo, config: { accountName: '', accountKey: '', container: repo.bucket, blobDomain: repo.endpoint } };
    if (repo.type === 'Google Cloud' || repo.type === 'GCS') return { ...repo, type: 'Google Cloud', config: { bucket: repo.bucket, region: repo.region, serviceAccountKey: '' } };
    return { ...repo, config: { nfsServer: repo.endpoint.replace(/^nfs:\/\//, '').split(':')[0] || '', nfsPath: repo.bucket } };
  }

  const storageTypeOptions = [
    { type: 'S3', title: 'Amazon S3', icon: Database, color: 'bg-amber-50 text-amber-600 border-amber-100' },
    { type: 'S3-Compatible', title: 'S3 Compatible', icon: Database, color: 'bg-indigo-50 text-indigo-600 border-indigo-100' },
    { type: 'Azure', title: 'Azure Blob', icon: Cloud, color: 'bg-blue-50 text-blue-600 border-blue-100' },
    { type: 'Google Cloud', title: 'Google Cloud', icon: Cloud, color: 'bg-rose-50 text-rose-600 border-rose-100' },
  ];

  const createStorageDraft = (type: string): StorageRepo => {
    const base = {
      id: 'repo-' + Date.now(),
      name: '',
      type,
      endpoint: '',
      bucket: '',
      region: type === 'NFS' ? 'local' : '',
      useTls: type !== 'NFS',
      status: 'warning' as const,
      updatedAt: new Date().toISOString(),
    };
    if (type === 'S3') return { ...base, config: { bucket: '', region: '', accessKey: '', secretKey: '' } };
    if (type === 'S3-Compatible') return { ...base, config: { bucket: '', region: '', endpoint: '', accessKey: '', secretKey: '', useSsl: true, urlStyle: 'path' } };
    if (type === 'Azure') return { ...base, region: '', config: { accountName: '', accountKey: '', container: '', blobDomain: 'blob.core.windows.net' } };
    if (type === 'Google Cloud') return { ...base, config: { bucket: '', region: '', serviceAccountKey: '' } };
    return { ...base, useTls: false, config: { nfsServer: '', nfsPath: '' } };
  };

  const storageConfigValue = (key: string) => String(editingRepo?.config?.[key] ?? '');

  const updateEditingConfig = (key: string, value: string | boolean) => {
    if (!editingRepo) return;
    const config = { ...(editingRepo.config || {}), [key]: value };
    const patch: Partial<StorageRepo> = { config };
    if (key === 'bucket' || key === 'container' || key === 'nfsPath') patch.bucket = String(value);
    if (key === 'region') patch.region = String(value || '');
    if (key === 'endpoint' || key === 'blobDomain') patch.endpoint = String(value);
    if (key === 'useSsl') patch.useTls = Boolean(value);
    if (key === 'nfsServer' || key === 'nfsPath') {
      const server = String(key === 'nfsServer' ? value : config.nfsServer || '');
      const nfsPath = String(key === 'nfsPath' ? value : config.nfsPath || '');
      patch.endpoint = server && nfsPath ? 'nfs://' + server + ':' + nfsPath : server;
    }
    setEditingRepo({ ...editingRepo, ...patch });
  };

  const storageReady = (repo: StorageRepo | null) => {
    if (!repo?.name.trim()) return false;
    const c = repo.config || {};
    const alreadySaved = Boolean(repo.id && !repo.id.startsWith('repo-'));
    if (repo.type === 'S3') return Boolean(c.bucket && c.region && (alreadySaved || c.accessKey && c.secretKey));
		if (isS3CompatibleType(repo.type)) return Boolean(c.bucket && c.endpoint && (alreadySaved || c.accessKey && c.secretKey));
    if (repo.type === 'Azure') return Boolean(c.accountName && c.accountKey && c.container);
    if (repo.type === 'Google Cloud') return Boolean(c.bucket && c.serviceAccountKey);
    if (repo.type === 'NFS') return Boolean(c.nfsServer && c.nfsPath);
    return false;
  };

  const storageQueryValue = (repo: StorageRepo, field: string) => {
    if (field === 'type') return repo.type;
    if (field === 'bucket') return repo.bucket || '';
    if (field === 'endpoint') return repo.endpoint || '';
    if (field === 'region') return repo.region || '';
    if (field === 'tls') return repo.useTls ? 'SSL TLS enabled' : 'SSL TLS disabled off';
    if (field === 'urlStyle') return repo.urlStyle === 'virtual' ? 'Virtual-host' : 'Path';
    if (field === 'status') return repo.status;
    if (field === 'lastValidatedAt') return repo.lastValidatedAt ? formatDateTime(repo.lastValidatedAt) : 'Never';
    return repo.name;
  };
  const storageMatchesFilter = (repo: StorageRepo, filter: string) => {
    if (filter === 'connected') return repo.status === 'connected';
    if (filter === 'warning') return repo.status !== 'connected';
    if (filter === 'tls') return repo.useTls;
    if (filter === 'noTls') return !repo.useTls;
    return true;
  };
  const filteredRepos = repos.filter(repo => {
    const keyword = query.trim().toLowerCase();
    const queryMatched = !keyword || storageQueryValue(repo, queryField).toLowerCase().includes(keyword);
    const tagsMatched = activeTags.length === 0 || activeTags.includes(repo.type);
    const filtersMatched = activeFilters.length === 0 || activeFilters.every(filter => {
      if (parseColumnFilterToken(filter)) return matchesColumnFilterToken(filter, field => storageQueryValue(repo, field));
      return storageMatchesFilter(repo, filter);
    });
    return queryMatched && tagsMatched && filtersMatched;
  });
  const storageColumns = [
    { value: 'type', label: 'Type', minWidth: 150 },
    { value: 'bucket', label: 'Bucket', minWidth: 140 },
    { value: 'region', label: 'Region', minWidth: 120 },
    { value: 'endpoint', label: 'Endpoint', minWidth: 200 },
    { value: 'tls', label: 'SSL', minWidth: 78 },
    { value: 'urlStyle', label: 'URL Style', minWidth: 110 },
    { value: 'status', label: 'Status', minWidth: 100 },
    { value: 'lastValidatedAt', label: 'Last Verified', minWidth: 150 },
  ];
  const storageQueryFields = listToolbarQueryFields([{ value: 'name', label: 'Repository Name' }], storageColumns, visibleColumns);
  const selectedRepos = repos.filter(repo => selectedRepoIds.includes(repo.id));
  const singleSelectedRepo = selectedRepos.length === 1 ? selectedRepos[0] : null;
  const allVisibleReposSelected = filteredRepos.length > 0 && filteredRepos.every(repo => selectedRepoIds.includes(repo.id));

  const toggleSelectedRepo = (repoId: string) => {
    setSelectedRepoIds(prev => prev.includes(repoId) ? prev.filter(id => id !== repoId) : [...prev, repoId]);
  };

  const toggleVisibleRepos = () => {
    setSelectedRepoIds(prev => {
      const visibleIds = filteredRepos.map(repo => repo.id);
      if (visibleIds.length === 0) return prev;
      if (visibleIds.every(id => prev.includes(id))) return prev.filter(id => !visibleIds.includes(id));
      return Array.from(new Set([...prev, ...visibleIds]));
    });
  };

  const storageTableColumns = useMemo<HyperTableColumn<StorageRepo>[]>(() => {
    const columns: HyperTableColumn<StorageRepo>[] = [
      {
        id: 'select',
        header: () => (
          <input
            type="checkbox"
            checked={allVisibleReposSelected}
            onClick={event => event.stopPropagation()}
            onChange={toggleVisibleRepos}
          />
        ),
        cell: info => (
          <input
            type="checkbox"
            checked={selectedRepoIds.includes(info.row.original.id)}
            onClick={event => event.stopPropagation()}
            onChange={() => toggleSelectedRepo(info.row.original.id)}
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
        header: 'Repository Name',
        accessorFn: repo => repo.name,
        size: 260,
        minSize: 190,
        maxSize: 520,
        cell: info => {
          const repo = info.row.original;
          return (
            <div className="hbdr-dr-name-cell">
              <div className={'hbdr-dr-namespace-icon ' + repoIconClass(repo.type)}>
                <Database size={18} />
              </div>
              <div>
                <p className="hbdr-dr-app-name">{repo.name}</p>
              </div>
            </div>
          );
        },
        meta: { title: repo => repo.name },
      },
    ];
    const addColumn = (column: HyperTableColumn<StorageRepo>) => {
      if (visibleColumns.includes(column.id as string)) columns.push(column);
    };
    addColumn({
      id: 'type',
      header: 'Type',
      accessorFn: repo => repo.type || '',
      size: 150,
      minSize: 120,
      cell: info => <span className="hbdr-dr-storage">{info.row.original.type || 'N/A'}</span>,
      meta: { kind: 'status', title: repo => repo.type || 'N/A' },
    });
    addColumn({
      id: 'bucket',
      header: 'Bucket',
      accessorFn: repo => repo.bucket || '',
      size: 180,
      minSize: 140,
      cell: info => info.row.original.bucket || <span className="hbdr-dr-na">N/A</span>,
      meta: { kind: 'secondary', title: repo => repo.bucket || 'N/A' },
    });
    addColumn({
      id: 'region',
      header: 'Region',
      accessorFn: repo => repo.region || '',
      size: 130,
      minSize: 110,
      cell: info => info.row.original.region || <span className="hbdr-dr-na">N/A</span>,
      meta: { kind: 'secondary', title: repo => repo.region || 'N/A' },
    });
    addColumn({
      id: 'endpoint',
      header: 'Endpoint',
      accessorFn: repo => repo.endpoint || '',
      size: 260,
      minSize: 180,
      maxSize: 520,
      cell: info => info.row.original.endpoint || <span className="hbdr-dr-na">N/A</span>,
      meta: { kind: 'secondary', title: repo => repo.endpoint || 'N/A' },
    });
    addColumn({
      id: 'tls',
      header: 'SSL',
      accessorFn: repo => repo.useTls ? 1 : 0,
      size: 90,
      minSize: 78,
      cell: info => info.row.original.useTls
        ? <span className="hbdr-dr-ssl hbdr-dr-ssl-on"><Lock size={11} />SSL</span>
        : <span className="hbdr-dr-ssl hbdr-dr-ssl-off">Off</span>,
      meta: { kind: 'status', title: repo => repo.useTls ? 'SSL enabled' : 'SSL disabled' },
    });
    addColumn({
      id: 'urlStyle',
      header: 'URL Style',
      accessorFn: repo => repo.urlStyle || '',
      size: 126,
      minSize: 110,
      cell: info => <span className="hbdr-dr-url-style">{info.row.original.urlStyle === 'virtual' ? 'Virtual-host' : 'Path'}</span>,
      meta: { kind: 'status', title: repo => repo.urlStyle === 'virtual' ? 'Virtual-host' : 'Path' },
    });
    addColumn({
      id: 'status',
      header: 'Status',
      accessorFn: repo => repo.status || '',
      size: 126,
      minSize: 100,
      cell: info => {
        const repo = info.row.original;
        return (
          <span className={
            repo.status === 'connected' ? 'hbdr-dr-task-ok'
              : repo.status === 'warning' ? 'hbdr-dr-task-warn'
                : 'hbdr-dr-task-unknown'
          }>
            {repo.status === 'connected' ? 'CONNECTED' : repo.status === 'warning' ? 'WARNING' : 'UNKNOWN'}
          </span>
        );
      },
      meta: { kind: 'status', title: repo => repo.status || 'unknown' },
    });
    addColumn({
      id: 'lastValidatedAt',
      header: 'Last Verified',
      accessorFn: repo => repo.lastValidatedAt || '',
      size: 168,
      minSize: 150,
      maxSize: 260,
      cell: info => (
        <span className="hbdr-dr-last-verified">
          {info.row.original.lastValidatedAt ? formatDateTime(info.row.original.lastValidatedAt) : <span className="hbdr-dr-na">Never</span>}
        </span>
      ),
      meta: { kind: 'secondary', title: repo => repo.lastValidatedAt ? formatDateTime(repo.lastValidatedAt) : 'Never' },
    });
    return columns;
  }, [allVisibleReposSelected, selectedRepoIds, visibleColumns]);

  const closeStorageWizard = () => {
    setStorageTypeOpen(false);
    setEditingRepo(null);
  };

	const saveStorage = async () => {
		if (!editingRepo || !storageReady(editingRepo)) return;
		setSavingStorage(true);
		setStorageError(null);
		try {
			const created = await apiPost<ApiStorageRepo>('/api/v1/storage-repositories', buildStorageRepositoryInput(editingRepo));
			const saved = normalizeStorageRepo(mapStorageRepo(created));
			setRepos(prev => prev.some(repo => repo.id === saved.id) ? prev.map(repo => repo.id === saved.id ? saved : repo) : [saved, ...prev]);
			onStorageCreated?.(saved);
			closeStorageWizard();
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to save storage repository');
		} finally {
			setSavingStorage(false);
		}
	};

	const saveEditedStorage = async () => {
    if (!editingRepo || !storageReady(editingRepo)) return;
		setSavingStorage(true);
		setStorageError(null);
		try {
			const updated = await apiPatch<ApiStorageRepo>(`/api/v1/storage-repositories/${editingRepo.id}`, buildStorageRepositoryInput(editingRepo));
			const saved = normalizeStorageRepo(mapStorageRepo(updated));
			setRepos(prev => prev.map(repo => repo.id === saved.id ? saved : repo));
			onStorageCreated?.(saved);
			setEditingRepo(null);
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to update storage repository');
		} finally {
			setSavingStorage(false);
		}
	};

	const syncSelectedStorage = async () => {
		if (selectedRepos.length === 0 || clusters.length === 0) return;
		setSyncingStorage(true);
		setStorageError(null);
		try {
			await Promise.all(selectedRepos.flatMap(repo =>
				clusters.map(cluster => apiPost<ApiTask>(`/api/v1/storage-repositories/${repo.id}/sync`, {
					clusterId: cluster.id,
				})),
			));
			const timestamp = new Date().toISOString();
			setRepos(prev => prev.map(repo => selectedRepoIds.includes(repo.id) ? { ...repo, status: 'connected', updatedAt: timestamp } : repo));
			setStorageBulkMenuOpen(false);
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to sync storage repository');
		} finally {
			setSyncingStorage(false);
		}
	};

	const testStorageConnection = async (repoId: string) => {
		setStorageError(null);
		try {
			const result = await apiPost<{ status: string; detail: string; repository: ApiStorageRepo }>(
				`/api/v1/storage-repositories/${repoId}/test`,
				{},
			);
			const updated = normalizeStorageRepo(mapStorageRepo(result.repository));
			setRepos(prev => prev.map(repo => repo.id === repoId ? updated : repo));
			setDetailRepo(prev => (prev && prev.id === repoId ? updated : prev));
			setEditingRepo(prev => (prev && prev.id === repoId ? updated : prev));
			return result;
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Test connection failed';
			setStorageError(message);
			throw error;
		}
	};

	const testSelectedStorage = async () => {
		if (selectedRepos.length === 0) return;
		setSyncingStorage(true);
		try {
			await Promise.all(selectedRepos.map(repo => testStorageConnection(repo.id).catch(() => null)));
			setStorageBulkMenuOpen(false);
		} finally {
			setSyncingStorage(false);
		}
	};

  const closeEditStorage = () => {
    setEditingRepo(null);
    setStorageTestMessage(null);
  };

  const runSavedStorageConnectionTest = async () => {
    if (!editingRepo) return;
    setSyncingStorage(true);
    setStorageTestMessage(null);
    try {
      const result = await testStorageConnection(editingRepo.id);
      setStorageTestMessage({
        tone: result.status === 'connected' ? 'ok' : 'fail',
        text: result.detail || (result.status === 'connected' ? 'Reachability OK' : 'Test failed'),
      });
      setStorageError(null);
    } catch (error) {
      setStorageTestMessage({ tone: 'fail', text: error instanceof Error ? error.message : 'Test connection failed' });
    } finally {
      setSyncingStorage(false);
    }
  };


  const deleteStorage = async () => {
    if (!deleteRepo) return;
		setSavingStorage(true);
		setStorageError(null);
		try {
			await apiDelete(`/api/v1/storage-repositories/${deleteRepo.id}`);
			setRepos(prev => prev.filter(repo => repo.id !== deleteRepo.id));
			setSelectedRepoIds(prev => prev.filter(id => id !== deleteRepo.id));
			setDeleteRepo(null);
		} catch (error) {
			setStorageError(error instanceof Error ? error.message : 'Failed to delete storage repository');
		} finally {
			setSavingStorage(false);
		}
  };

  function repoIconClass(type: string) {
    if (type.toLowerCase().includes('s3')) return 'bg-amber-50 text-amber-600 border-amber-100';
    if (type === 'Azure') return 'bg-blue-50 text-blue-600 border-blue-100';
    if (type === 'Google Cloud') return 'bg-rose-50 text-rose-600 border-rose-100';
    return 'bg-slate-50 text-slate-600 border-slate-200';
  }

  const renderStorageFields = (allowTypeChange: boolean) => {
    if (!editingRepo) return null;
    return (
      <div className="space-y-3">
        <div className={allowTypeChange ? 'grid grid-cols-1 gap-4' : 'grid grid-cols-1 gap-4 md:grid-cols-2'}>
          <EditField label="Name" value={editingRepo.name} placeholder="My Backup Repo" onChange={value => setEditingRepo({ ...editingRepo, name: value })} />
          {!allowTypeChange && (
            <label className="flex flex-col gap-1.5 text-xs font-semibold tracking-normal text-slate-600">
              Type
              <div className="flex h-10 items-center rounded-lg border border-slate-200 bg-slate-50 px-3.5 text-xs font-bold uppercase text-slate-600">
                <span>{editingRepo.type}</span>
              </div>
            </label>
          )}
        </div>

		{(editingRepo.type === 'S3' || isS3CompatibleType(editingRepo.type)) && (
          <div className="hbdr-storage-field-stack">
            {isS3CompatibleType(editingRepo.type) && (
              <EditField label="Endpoint (ENDPOINT)" value={storageConfigValue('endpoint')} placeholder="http://minio:9000" onChange={value => updateEditingConfig('endpoint', value)} />
            )}
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="ACCESS KEY ID (AK)" value={storageConfigValue('accessKey')} placeholder="AKIA..." onChange={value => updateEditingConfig('accessKey', value)} />
              <EditField label="SECRET ACCESS KEY (SK)" type="password" value={storageConfigValue('secretKey')} placeholder="Enter secret access key" onChange={value => updateEditingConfig('secretKey', value)} />
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Bucket Name" value={storageConfigValue('bucket')} placeholder="Enter bucket name" onChange={value => updateEditingConfig('bucket', value)} />
              <EditField label="Region (REGION)" value={storageConfigValue('region')} placeholder="us-west-2" onChange={value => updateEditingConfig('region', value)} />
            </div>
            {isS3CompatibleType(editingRepo.type) && (() => {
              const ssl = Boolean(editingRepo.config?.useSsl ?? editingRepo.useTls);
              const urlStyle = storageConfigValue('urlStyle') || 'path';
              return (
                <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 bg-white px-3.5 py-2.5">
                    <div className="flex items-center gap-2.5">
                      <span className={'flex h-7 w-7 shrink-0 items-center justify-center rounded-md border transition-colors ' + (ssl ? 'border-emerald-100 bg-emerald-50 text-emerald-600' : 'border-slate-200 bg-slate-50 text-slate-400')}>
                        <ShieldCheck size={13} />
                      </span>
                      <div>
                        <p className="text-[11px] font-bold uppercase tracking-wider text-slate-700">SSL/TLS</p>
                        <p className={'text-[10px] font-semibold ' + (ssl ? 'text-emerald-600' : 'text-slate-400')}>{ssl ? 'Encrypted' : 'Disabled'}</p>
                      </div>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={ssl}
                      onClick={() => updateEditingConfig('useSsl', !ssl)}
                      className={
                        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-300 ' +
                        (ssl ? 'border-emerald-500 bg-emerald-500' : 'border-slate-200 bg-slate-200')
                      }
                    >
                      <span className={'inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ' + (ssl ? 'translate-x-4' : 'translate-x-0.5')} />
                    </button>
                  </div>
                  <div className="flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                    <p className="shrink-0 text-[11px] font-bold uppercase tracking-wider text-slate-700">URL Style</p>
                    <div className="grid flex-1 grid-cols-2 gap-1">
                      {[
                        { value: 'path', label: 'Path' },
                        { value: 'virtual', label: 'Virtual-host' },
                      ].map(opt => {
                        const active = urlStyle === opt.value;
                        return (
                          <button
                            type="button"
                            key={opt.value}
                            onClick={() => updateEditingConfig('urlStyle', opt.value)}
                            className={
                              'rounded-md px-2 py-1 text-[11px] font-bold transition-all ' +
                              (active
                                ? 'bg-blue-50 text-blue-700 ring-1 ring-blue-200'
                                : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700')
                            }
                          >
                            {opt.label}
                        </button>
                        );
                      })}
                    </div>
                  </div>
                </div>
              );
            })()}
          </div>
        )}

        {editingRepo.type === 'Azure' && (
          <div className="hbdr-storage-field-stack">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Storage Account Name" value={storageConfigValue('accountName')} placeholder="mystorageaccount" onChange={value => updateEditingConfig('accountName', value)} />
              <EditField label="Account Key" type="password" value={storageConfigValue('accountKey')} placeholder="Azure Storage Account Key" onChange={value => updateEditingConfig('accountKey', value)} />
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Container Name" value={storageConfigValue('container')} placeholder="my-backups" onChange={value => updateEditingConfig('container', value)} />
              <EditField label="Endpoint Suffix" value={storageConfigValue('blobDomain')} placeholder="blob.core.windows.net" onChange={value => updateEditingConfig('blobDomain', value)} />
            </div>
          </div>
        )}

        {editingRepo.type === 'Google Cloud' && (
          <div className="hbdr-storage-field-stack">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <EditField label="Bucket Name" value={storageConfigValue('bucket')} placeholder="Enter bucket name" onChange={value => updateEditingConfig('bucket', value)} />
              <EditField label="Region" value={storageConfigValue('region')} placeholder="us-central1" onChange={value => updateEditingConfig('region', value)} />
            </div>
            <label className="flex flex-col gap-1.5 text-xs font-semibold tracking-normal text-slate-600">
              SERVICE ACCOUNT KEY
              <textarea value={storageConfigValue('serviceAccountKey')} onChange={event => updateEditingConfig('serviceAccountKey', event.target.value)} placeholder={'{ "type": "service_account", ... }'} rows={4} className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 font-mono text-xs text-slate-700 outline-none transition-all focus:border-blue-500 focus:ring-2 focus:ring-blue-100" />
            </label>
          </div>
        )}

        {editingRepo.type === 'NFS' && (
          <div className="grid grid-cols-1 gap-4">
            <EditField label="NFS Server Address" value={storageConfigValue('nfsServer')} placeholder="192.168.1.100" onChange={value => updateEditingConfig('nfsServer', value)} />
            <EditField label="Mount Path" value={storageConfigValue('nfsPath')} placeholder="/mnt/backups" onChange={value => updateEditingConfig('nfsPath', value)} />
          </div>
        )}
      </div>
    );
  };

  const handleStorageConnectionTest = async () => {
    if (!editingRepo) return;
    const isDraft = !editingRepo.id || editingRepo.id.startsWith('repo-');
    if (isDraft) {
      if (!editingRepo.endpoint || !editingRepo.bucket) {
        setStorageTestMessage({ tone: 'fail', text: 'Enter endpoint and bucket first.' });
        return;
      }
      setSyncingStorage(true);
      setStorageTestMessage(null);
      try {
        const input = buildStorageRepositoryInput(editingRepo);
        const result = await apiPost<{ status: string; detail: string }>('/api/v1/storage-repositories/test', input);
        setStorageTestMessage(result.status === 'connected'
          ? { tone: 'ok', text: `Reachability OK: ${result.detail}` }
          : { tone: 'fail', text: result.detail || 'Reachability test failed' });
      } catch (e) {
        setStorageTestMessage({ tone: 'fail', text: e instanceof Error ? e.message : 'Test connection failed' });
      } finally {
        setSyncingStorage(false);
      }
      return;
    }
    setSyncingStorage(true);
    try {
      const result = await testStorageConnection(editingRepo.id);
      setStorageTestMessage({ tone: result.status === 'connected' ? 'ok' : 'fail', text: result.detail || (result.status === 'connected' ? 'Reachability OK' : 'Test failed') });
      setStorageError(null);
    } catch (e) {
      setStorageTestMessage({ tone: 'fail', text: e instanceof Error ? e.message : 'Test connection failed' });
    } finally {
      setSyncingStorage(false);
    }
  };

  return (
    <motion.div key="storage" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="space-y-5">
      <div className="hbdr-page-hero">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-blue-600 shadow-sm"><Archive size={18} /></div>
            <div className="hbdr-storage-page-title">
              <h3 className="text-xs font-black uppercase tracking-tight text-slate-800">Storage</h3>
              <p className="mt-0.5 text-[11px] font-medium text-slate-400">Maintain shared restore-point repositories across clusters.</p>
            </div>
          </div>
          <div />
        </div>
      </div>

      <div className="hbdr-dr-table-card hbdr-storage-table-list">
        <div className="hbdr-dr-table-head">
            <div className="hbdr-dr-toolbar">
              <div className="hbdr-dr-action-group">
                <button aria-label="Create storage repository" title="Create storage repository" onClick={() => { setEditingRepo(createStorageDraft('S3-Compatible')); setStorageTestMessage(null); setStorageError(null); setStorageTypeOpen(true); }} className="hbdr-dr-action-primary">New</button>
              <div className="relative">
                <button disabled={selectedRepos.length === 0} onClick={() => setStorageBulkMenuOpen(prev => !prev)} className="hbdr-dr-more">
                  More <ChevronDown size={15} className={storageBulkMenuOpen ? 'rotate-180 transition-transform' : 'transition-transform'} />
                </button>
                <AnimatePresence>
                  {storageBulkMenuOpen && selectedRepos.length > 0 && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setStorageBulkMenuOpen(false)} />
                      <motion.div initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute left-0 top-11 z-40 w-48 overflow-hidden rounded-2xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/80 ring-1 ring-slate-950/5">
                        <button disabled={!singleSelectedRepo} onClick={() => { if (!singleSelectedRepo) return; setDetailRepo(normalizeStorageRepo(singleSelectedRepo)); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Eye size={14} />View</button>
                        <button disabled={!singleSelectedRepo} onClick={() => { if (!singleSelectedRepo) return; setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(singleSelectedRepo)); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Settings size={14} />Edit</button>
                        <button disabled={!singleSelectedRepo} onClick={() => { if (!singleSelectedRepo) return; setDeleteRepo(singleSelectedRepo); setStorageBulkMenuOpen(false); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50 disabled:cursor-not-allowed disabled:bg-slate-50/70 disabled:text-slate-300"><Trash2 size={14} />Delete</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
            <ListToolbarControls
              query={query}
              setQuery={setQuery}
              queryField={queryField}
              setQueryField={setQueryField}
              queryFields={storageQueryFields}
              tags={storageTypeOptions.map(option => ({ value: option.type, label: option.title, count: repos.filter(repo => repo.type === option.type).length }))}
              activeTags={activeTags}
              setActiveTags={setActiveTags}
              filters={[
                { value: 'connected', label: 'Connected', count: repos.filter(repo => storageMatchesFilter(repo, 'connected')).length },
                { value: 'warning', label: 'Warning', count: repos.filter(repo => storageMatchesFilter(repo, 'warning')).length },
                { value: 'tls', label: 'TLS Enabled', count: repos.filter(repo => storageMatchesFilter(repo, 'tls')).length },
                { value: 'noTls', label: 'TLS Disabled', count: repos.filter(repo => storageMatchesFilter(repo, 'noTls')).length },
              ]}
              activeFilters={activeFilters}
              setActiveFilters={setActiveFilters}
              columns={storageColumns}
              visibleColumns={visibleColumns}
              setVisibleColumns={setVisibleColumns}
              onRefresh={() => {
                const timestamp = new Date().toISOString();
                setRepos(prev => prev.map(repo => ({ ...repo, updatedAt: timestamp })));
                setSelectedRepoIds([]);
              }}
            />
          </div>
          {storageError && <p className="mt-3 text-xs font-bold text-amber-600">Storage operation warning: {storageError}</p>}
        </div>
        <HyperTable
          variant="page"
          density="comfortable"
          columns={storageTableColumns}
          data={filteredRepos}
          getRowId={row => row.id}
          onRowClick={row => toggleSelectedRepo(row.id)}
          getRowClassName={row => selectedRepoIds.includes(row.id) ? 'hbdr-dr-row-selected' : ''}
          selectedCount={selectedRepoIds.length}
          emptyMessage="No matching storage repositories"
          className="hbdr-storage-hyper-table"
        />
      </div>

      <div className="hidden grid-cols-1 gap-4">
        {filteredRepos.map(repo => (
          <motion.div
            key={repo.id}
            whileHover={{ y: -2 }}
            className={`group relative flex flex-col gap-5 overflow-visible rounded-2xl border border-slate-200 bg-white p-6 shadow-sm transition-all hover:border-slate-300 hover:shadow-md lg:flex-row lg:items-center lg:justify-between ${menuId === repo.id ? 'z-40' : 'z-0'}`}
          >
            <div className="flex min-w-0 items-center gap-5">
              <div className={'flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl border shadow-sm transition-transform group-hover:scale-105 ' + repoIconClass(repo.type)}>
                <Database size={24} />
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="truncate text-lg font-bold tracking-tight text-slate-900">{repo.name}</h3>
                  <span className="rounded-full bg-slate-100 px-2.5 py-1 font-mono text-[10px] font-bold uppercase text-slate-500">{repo.type}</span>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-sm font-medium text-slate-500">
                  <span className="flex items-center gap-1.5"><Archive size={14} className="text-slate-400" />Bucket: {repo.bucket || '-'}</span>
                  <span className="flex items-center gap-1.5"><Grid3X3 size={14} className="text-slate-400" />Region: {repo.region || 'N/A'}</span>
                </div>
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-6 lg:justify-end">
              <div className="text-right">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-slate-400">Status</p>
                <div className="flex items-center justify-end gap-1.5">
                  <span className={'h-2 w-2 rounded-full ' + (repo.status === 'connected' ? 'bg-emerald-500' : 'bg-amber-500')} />
                  <span className="text-xs font-bold uppercase text-slate-700">{repo.status === 'connected' ? 'CONNECTED' : 'WARNING'}</span>
                </div>
              </div>
              <div className="border-l border-slate-100 pl-6 text-right">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-slate-400">Last Verified</p>
                <p className="text-xs font-medium text-slate-600">{repo.updatedAt}</p>
              </div>
              <div className="relative">
                <button onClick={(event) => { event.stopPropagation(); setMenuId(menuId === repo.id ? null : repo.id); }} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-700"><MoreVertical size={18} /></button>
                <AnimatePresence>
                  {menuId === repo.id && (
                    <>
                      <div className="fixed inset-0 z-30" onClick={() => setMenuId(null)} />
                      <motion.div onClick={(event) => event.stopPropagation()} initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }} className="absolute right-0 top-10 z-50 w-40 overflow-hidden rounded-xl border border-slate-100 bg-white py-1 shadow-2xl shadow-slate-200/70 ring-1 ring-slate-950/5">
                        <button onClick={() => { setDetailRepo(normalizeStorageRepo(repo)); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Eye size={14} />View Details</button>
                        <button onClick={() => { setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(repo)); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-slate-600 hover:bg-slate-50"><Settings size={14} />Edit</button>
                        <button onClick={() => { setDeleteRepo(repo); setMenuId(null); }} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs font-bold text-rose-600 hover:bg-rose-50"><Trash2 size={14} />Delete</button>
                      </motion.div>
                    </>
                  )}
                </AnimatePresence>
              </div>
            </div>
          </motion.div>
        ))}
      </div>

      <AnimatePresence>
        {storageTypeOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={closeStorageWizard} />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-storage-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>New Storage Repository</strong>
                  <span>Create a repository for Velero backup and restore data.</span>
                </div>
                <button type="button" onClick={closeStorageWizard} aria-label="Close storage drawer"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-storage-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Repository Type</h4>
                  {(() => {
                    const currentType = editingRepo?.type ?? 'S3-Compatible';
                    return (
                      <div className="hbdr-advanced-filter-box hbdr-storage-type-select-box">
                        <label>
                          <span>Type</span>
                          <select
                            value={currentType}
                            onChange={event => {
                              const draft = createStorageDraft(event.target.value);
                              draft.name = editingRepo?.name ?? '';
                              setEditingRepo(draft);
                            }}
                          >
                            {storageTypeOptions.map(option => (
                              <option key={option.type} value={option.type}>{option.title}</option>
                            ))}
                          </select>
                        </label>
                      </div>
                    );
                  })()}
                </section>

                {editingRepo && (
                  <section className="hbdr-advanced-filter-section">
                    <h4>Configuration</h4>
                    <div className="hbdr-advanced-filter-box hbdr-storage-config-box">{renderStorageFields(true)}</div>
                    <div className="hbdr-storage-connection-check">
                      {storageTestMessage && (
                        <div className={`hbdr-storage-test-result ${storageTestMessage.tone === 'ok' ? 'is-ok' : 'is-fail'}`}>
                          {storageTestMessage.text}
                        </div>
                      )}
                      <button type="button" onClick={handleStorageConnectionTest} disabled={syncingStorage} className="hbdr-storage-test-button">
                        <Activity size={14} />{syncingStorage ? 'Testing...' : 'Test Connection'}
                      </button>
                    </div>
                  </section>
                )}
              </div>
              {editingRepo && (
                <div className="hbdr-storage-drawer-footer">
                  <div className="hbdr-filter-drawer-actions hbdr-storage-drawer-actions">
                    <button type="button" onClick={saveStorage} disabled={!storageReady(editingRepo) || savingStorage}>{savingStorage ? "Saving..." : "Create Storage"}</button>
                    <button type="button" onClick={closeStorageWizard}>Cancel</button>
                  </div>
                </div>
              )}
            </motion.div>
          </>
        )}

        {detailRepo && (
          <ModalFrame title="Storage Repository Details" onClose={() => setDetailRepo(null)}>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <Info label="Name" value={detailRepo.name} />
              <Info label="Type" value={detailRepo.type} />
              <Info label="Endpoint" value={detailRepo.endpoint || '-'} />
              <Info label="Bucket Name" value={detailRepo.bucket || '-'} />
              <Info label="Region" value={detailRepo.region || 'N/A'} />
              <Info label="Use TLS" value={detailRepo.useTls ? 'Yes' : 'No'} />
              <Info label="Status" value={detailRepo.status === 'connected' ? 'CONNECTED' : detailRepo.status === 'warning' ? 'WARNING' : 'UNKNOWN'} />
              <Info label="Last Verified" value={detailRepo.lastValidatedAt ? formatDateTime(detailRepo.lastValidatedAt) : 'Never'} />
            </div>
            <div className="mt-5 flex justify-end gap-3">
              <button onClick={() => setDetailRepo(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Close</button>
              <button onClick={() => { setStorageTestMessage(null); setStorageError(null); setEditingRepo(normalizeStorageRepo(detailRepo)); setDetailRepo(null); }} className="rounded-xl bg-blue-600 px-6 py-2.5 text-sm font-bold text-white shadow-lg shadow-blue-100 transition-all hover:bg-blue-700">Edit Storage Repository</button>
            </div>
          </ModalFrame>
        )}

        {editingRepo && !storageTypeOpen && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="hbdr-filter-drawer-backdrop" onClick={closeEditStorage} />
            <motion.div initial={{ opacity: 0, x: 34 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 34 }} transition={{ duration: 0.18, ease: 'easeOut' }} className="hbdr-filter-drawer hbdr-storage-drawer">
              <div className="hbdr-filter-drawer-head">
                <div>
                  <strong>Edit Storage Repository</strong>
                  <span>Update repository connection and backup location settings.</span>
                </div>
                <button type="button" onClick={closeEditStorage} aria-label="Close storage drawer"><X size={18} /></button>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-storage-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Configuration</h4>
                  <div className="hbdr-advanced-filter-box hbdr-storage-config-box">{renderStorageFields(false)}</div>
                  <div className="hbdr-storage-connection-check">
                    {storageTestMessage && (
                      <div className={`hbdr-storage-test-result ${storageTestMessage.tone === 'ok' ? 'is-ok' : 'is-fail'}`}>
                        {storageTestMessage.text}
                      </div>
                    )}
                    <button type="button" onClick={runSavedStorageConnectionTest} disabled={syncingStorage} className="hbdr-storage-test-button">
                      <Activity size={14} />{syncingStorage ? 'Testing...' : 'Test Connection'}
                    </button>
                  </div>
                </section>
              </div>
              <div className="hbdr-storage-drawer-footer">
                <div className="hbdr-filter-drawer-actions hbdr-storage-drawer-actions">
                  <button type="button" onClick={()=>void saveEditedStorage()} disabled={!storageReady(editingRepo) || savingStorage}>{savingStorage ? 'Saving...' : 'Save Changes'}</button>
                  <button type="button" onClick={closeEditStorage}>Cancel</button>
                </div>
              </div>
            </motion.div>
          </>
        )}

        {deleteRepo && (
          <ModalFrame title="Delete Storage Repository" onClose={() => setDeleteRepo(null)}>
            <div className="space-y-5">
              <div className="rounded-2xl border border-rose-100 bg-rose-50 p-4 text-sm leading-6 text-rose-700">
                Confirm storage repository deletion <strong>{deleteRepo.name}</strong>? After deletion, this repository can no longer be used as a new DR recovery target.
              </div>
              {storageError && <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">{storageError}</div>}
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Info label="Name" value={deleteRepo.name} />
                <Info label="Type" value={deleteRepo.type} />
                <Info label="Bucket Name" value={deleteRepo.bucket || '-'} />
                <Info label="Last Verified" value={deleteRepo.lastValidatedAt ? formatDateTime(deleteRepo.lastValidatedAt) : 'Never'} />
              </div>
              <div className="flex justify-end gap-3">
                <button onClick={() => setDeleteRepo(null)} className="rounded-xl px-5 py-2 text-sm font-medium text-slate-600 transition-colors hover:bg-slate-50">Cancel</button>
                <button onClick={()=>void deleteStorage()} disabled={savingStorage} className="rounded-xl bg-rose-600 px-8 py-2.5 text-sm font-bold text-white shadow-lg shadow-rose-100 transition-all hover:bg-rose-700 active:scale-95 disabled:opacity-50">{savingStorage ? 'Deleting...' : 'Delete'}</button>
              </div>
            </div>
          </ModalFrame>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
