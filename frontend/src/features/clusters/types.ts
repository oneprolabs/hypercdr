export type ClusterStatus = 'healthy' | 'warning' | 'syncing';

type ApplicationStage = 'select' | 'config' | 'run';

export interface AppItem {
  apiId?: string;
  clusterId?: string;
  name: string;
  namespace: string;
  status: 'Active' | 'Running' | 'Terminating' | 'Unknown' | 'Protected';
  namespaceStatus?: string;
  policy?: string;
  storage?: string;
  targetCluster?: string;
  isProtected: boolean;
  lastBackup?: string;
  labels?: Record<string, string>;
  tags?: string[];
  workloadCount?: number;
  serviceCount?: number;
  ingressCount?: number;
  configMapCount?: number;
  secretCount?: number;
  pvcCount?: number;
  pvCapacityBytes?: number;
  resourceSummary?: ResourceSummary;
  protectionStatus?: string;
  protectionPlanId?: string;
  protectionPlanCreatedAt?: string;
  stage?: ApplicationStage;
  memberApps?: AppItem[];
  isMergedPlan?: boolean;
}

export type ResourceCategoryKey = 'workloads' | 'network' | 'storage' | 'config' | 'access' | 'jobs' | 'scaling' | 'policy' | 'other';

export type ResourceRef = {
  name: string;
  namespace?: string;
  kind?: string;
  apiVersion?: string;
  labels?: Record<string, string>;
  ready?: string;
  desiredReplicas?: number;
  readyReplicas?: number;
  updatedReplicas?: number;
  availableReplicas?: number;
  ageSeconds?: number;
  containers?: string[];
  images?: string[];
  selector?: string;
  fields?: Record<string, string>;
};

export type LabelResourceMatch = {
  id: string;
  name: string;
  namespace: string;
  kind: string;
  category: ResourceCategoryKey | 'namespace';
  labels: Record<string, string>;
};

export type LabelSelectorOption = {
  key: string;
  value: string;
  namespaceNames: string[];
  resources: LabelResourceMatch[];
  summary: string;
};

export type ResourceKindSummary = {
  kind: string;
  shortName?: string;
  count: number;
  resources?: ResourceRef[];
};

export type ResourceCategory = {
  key: ResourceCategoryKey | string;
  label: string;
  total: number;
  items?: ResourceKindSummary[];
};

export type ResourceSummary = {
  deployments?: number;
  statefulsets?: number;
  daemonsets?: number;
  jobs?: number;
  cronjobs?: number;
  services?: number;
  ingresses?: number;
  networkPolicies?: number;
  configmaps?: number;
  secrets?: number;
  serviceAccounts?: number;
  pvcs?: number;
  pvCapacityBytes?: number;
  drSupport?: DRSupportSummary;
  ageSeconds?: number;
  categories?: ResourceCategory[];
};

export type DRSupportSummary = {
  status?: 'supported' | 'unsupported' | 'warning' | string;
  reason?: string;
  checks?: DRSupportCheck[];
};

export type DRSupportCheck = {
  kind?: string;
  name?: string;
  status?: string;
  reason?: string;
  storageClass?: string;
  provisioner?: string;
  volume?: string;
  volumeType?: string;
};

export interface Cluster {
  id: string;
  name: string;
  region: string;
  version: string;
  status: ClusterStatus;
  connectionStatus?: string;
  compliance: number;
  nodes: number;
  nodeDetails?: ClusterNode[];
  storageClasses?: ClusterStorageClass[];
  restoreCachePolicy?: {
    mode: 'automatic' | string;
    enabled: boolean;
    storageClass?: string;
    residentThresholdMB?: number;
    cacheLimitMB?: number;
    reason?: string;
  };
  apiResources?: ClusterAPIResource[];
  namespaceApis?: ClusterNamespaceAPI[];
  namespaces: number;
  applications: number;
  agentVersion: string;
  latestAgentVersion: string;
  agentImage?: string;
  agentImageDigest?: string;
  latestAgentImage?: string;
  latestAgentImageDigest?: string;
  agentUpgradeAvailable?: boolean;
  agentUpgradeStatus?: string;
  agentUpgradeProgress?: number;
  veleroVersion?: string;
  veleroStatus?: string;
  veleroImage?: string;
  veleroImageDigest?: string;
  veleroServerReady?: boolean;
  veleroNodeAgentDesired?: number;
  veleroNodeAgentReady?: number;
  veleroNodeAgentImageDigest?: string;
  latestVeleroVersion?: string;
  latestVeleroImage?: string;
  latestVeleroImageDigest?: string;
  veleroUpgradeAvailable?: boolean;
  veleroUpgradeStatus?: string;
  veleroUpgradeProgress?: number;
  lastSeenAt?: string;
  role?: 'source' | 'target' | 'both';
  isDefault?: boolean;
  apps: AppItem[];
}

export type ClusterAPIResource = {
  group?: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
};

export type ClusterNamespaceAPI = {
  scope?: 'namespace' | 'cluster';
  namespace: string;
  group?: string;
  version: string;
  resource: string;
  kind: string;
  count: number;
};

export type ClusterNode = {
  name: string;
  status: string;
  roles?: string;
  ageSeconds?: number;
  kubeletVersion?: string;
  capacity?: Record<string, string>;
};

export type ClusterStorageClass = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  allowVolumeExpansion: string;
  default?: boolean;
  ageSeconds?: number;
};

export type ClusterNamespaceRow = {
  name: string;
  status: string;
  age: string;
};

export type ClusterNodeRow = {
  name: string;
  status: string;
  roles: string;
  age: string;
  version: string;
};

export type ClusterStorageClassRow = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  allowVolumeExpansion: string;
  age: string;
};
