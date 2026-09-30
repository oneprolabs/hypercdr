import React from 'react';
import { ShieldCheck } from 'lucide-react';
import { EditField } from '../../components/edit-field';
import { isS3CompatibleType, type StorageDraft } from './storage-form-model';
export function StorageFormFields({editingRepo,setEditingRepo,allowTypeChange}:{editingRepo:StorageDraft;setEditingRepo:React.Dispatch<React.SetStateAction<StorageDraft | null>>;allowTypeChange:boolean}) {
  const storageConfigValue = (key: string) => String(editingRepo?.config?.[key] ?? '');

  const updateEditingConfig = (key: string, value: string | boolean) => {
    if (!editingRepo) return;
    const config = { ...(editingRepo.config || {}), [key]: value };
    const patch: Partial<StorageDraft> = { config };
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
                              'hbdr-storage-url-style-option rounded-md px-2 py-1 text-[11px] font-bold transition-all ' +
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
}
