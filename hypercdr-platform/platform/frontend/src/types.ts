export interface StorageLocation {
  id: string;
  name: string;
  type: 'S3' | 'Azure' | 'GCS' | 'NFS' | 'S3-Compatible';
  config: {
    bucket?: string;
    region?: string;
    endpoint?: string;
    accessKey?: string;
    secretKey?: string;
    useSsl?: boolean;
    urlStyle?: 'path' | 'virtual';
    accountName?: string;
    accountKey?: string;
    container?: string;
    blobDomain?: string;
    serviceAccountKey?: string;
    nfsServer?: string;
    nfsPath?: string;
  };
  status: 'connected' | 'error';
  lastValidated: string;
}

export interface Cluster {
  id: string;
  name: string;
  status: 'healthy' | 'warning' | 'error' | 'syncing';
  region: string;
  version: string;
  lastBackup: string | null;
  compliance: number;
  applications: number;
  nodes: number;
  agentVersion: string;
  latestAgentVersion: string;
  apps?: any[];
}

export interface Operation {
  id: string;
  type: 'backup' | 'restore' | 'scan' | 'export';
  name: string;
  status: 'completed' | 'failed' | 'in-progress';
  time: string;
  cluster: string;
}

export interface ClusterDashboardData {
  summary: Cluster;
  stats: {
    totalDataProtected: string;
    backupSuccessRate: string;
    availableRestorePoints: number;
    unprotectedApplications: number;
  };
  recentOperations: Operation[];
}
