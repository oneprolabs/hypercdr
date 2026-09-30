export type StorageDraft={id:string;name:string;type:string;endpoint:string;bucket:string;region:string;useTls:boolean;status:'connected'|'warning'|'unknown';updatedAt:string;config?:Record<string,string|boolean>};
export type StorageRepositoryInput={name:string;type:string;endpoint:string;bucket:string;region:string;tlsEnabled:boolean;config:Record<string,string|boolean>;accessKey?:string;secretKey?:string;accountName?:string;accountKey?:string;serviceAccountKey?:string};
export const isS3CompatibleType=(type:string)=>['s3-compatible','s3 compatible'].includes(type.toLowerCase());
export const buildStorageRepositoryInput=(repo:StorageDraft):StorageRepositoryInput=>{const config=repo.config||{};const compatible=isS3CompatibleType(repo.type);const azure=repo.type==='Azure';const gcs=repo.type==='Google Cloud'||repo.type==='GCS';const domain=String(config.blobDomain||'blob.core.windows.net').replace(/^https?:\/\//,'');const endpoint=String(azure?`${String(config.accountName||'')}.${domain}`:config.endpoint||repo.endpoint||'');const bucket=String(azure?config.container||repo.bucket||'':config.bucket||repo.bucket||'');const rawRegion=String(config.region||repo.region||'').trim();const region=['n/a','na','-'].includes(rawRegion.toLowerCase())?'':rawRegion;const payloadConfig:Record<string,string|boolean>={};if(config.urlStyle)payloadConfig.urlStyle=String(config.urlStyle);if(config.prefix)payloadConfig.prefix=String(config.prefix);if(azure&&config.accountName)payloadConfig.storageAccount=String(config.accountName);return{name:repo.name,type:repo.type,endpoint,bucket,region,tlsEnabled:Boolean(config.useSsl??repo.useTls),config:payloadConfig,accessKey:String(config.accessKey||''),secretKey:String(config.secretKey||''),accountName:azure?String(config.accountName||''):undefined,accountKey:azure?String(config.accountKey||''):undefined,serviceAccountKey:gcs?String(config.serviceAccountKey||''):undefined}};

export const storageReady = (repo: StorageDraft | null) => {
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
export const createStorageRepository = <T,>(repo: StorageDraft): Promise<T> => {
  if (!storageReady(repo)) throw new Error('Complete the required repository fields.');
  return apiPost<T>('/api/v1/storage-repositories', buildStorageRepositoryInput(repo));
};
export const testStorageDraft = async (repo: StorageDraft): Promise<{ tone: 'ok' | 'fail'; text: string }> => {
  if (!storageReady(repo)) throw new Error('Complete the required repository fields first.');
  const result = await apiPost<{ status: string; detail: string }>('/api/v1/storage-repositories/test', buildStorageRepositoryInput(repo));
  return { tone: result.status === 'connected' ? 'ok' : 'fail', text: result.detail || (result.status === 'connected' ? 'Reachability OK' : 'Reachability test failed') };
};

export const createStorageDraft = (type: string): StorageDraft => {
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
import { apiPost } from '../../api/client';
