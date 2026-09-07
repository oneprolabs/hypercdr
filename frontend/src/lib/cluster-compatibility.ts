export function normalizeClusterType(clusterType?: string): 'kubernetes' | 'openshift' {
  return clusterType === 'openshift' ? 'openshift' : 'kubernetes';
}

export function clustersAreDRCompatible(sourceType?: string, targetType?: string): boolean {
  return normalizeClusterType(sourceType) === normalizeClusterType(targetType);
}

export function clusterCompatibilityMessage(sourceType?: string): string {
  return normalizeClusterType(sourceType) === 'openshift'
    ? 'OpenShift applications currently require an OpenShift target cluster.'
    : 'Native Kubernetes and Huawei Cloud CCE applications cannot currently use an OpenShift target cluster.';
}
