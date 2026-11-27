export const drillSteps = [
 ['Restore','Point and scope'],
 ['Target','Cluster and namespace'],
 ['Review','Confirm drill settings'],
] as const;
export function recoveryApplicationResources<Resource extends { apiVersion?: string; group?: string }>(resources: Resource[]): Resource[] {
 return resources.filter(item => item.group !== 'velero.io' && item.apiVersion?.split('/')[0] !== 'velero.io');
}
export function recoveryPackageNamespaces(resources: Array<{ namespace?: string; apiVersion?: string; group?: string; kind?: string }>) {
 return Array.from(new Set(recoveryApplicationResources(resources).map(item => item.namespace).filter((value): value is string => Boolean(value)))).sort();
}
export function drillNamespaceError(namespace:string) {
 return /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(namespace) && namespace.length<=63 ? '' : 'Namespace must be 1–63 lowercase letters, numbers or hyphens, and start and end with a letter or number.';
}
export function recoveryNamespaceMappings(namespaces: string[], config: {
 namespaceMode: string; targetNamespace: string; targetNamespaces?: Record<string, string>; restorePackageNamespaces?: string[];
}, suffix = 'drill') {
 const multiple = (config.restorePackageNamespaces?.length || namespaces.length) > 1;
 return namespaces.map(source => ({
  source,
  target: config.namespaceMode === 'original' ? source : multiple
   ? config.targetNamespaces?.[source] ?? `${source}-${suffix}`
   : config.namespaceMode === 'generated' ? `${source}-${suffix}` : config.targetNamespace,
 }));
}
export function recoveryNamespaceMappingError(mappings: Array<{source: string; target: string}>) {
 for (const mapping of mappings) {
  const error = drillNamespaceError(mapping.target);
  if (error) return `${mapping.source}: ${error}`;
 }
 return new Set(mappings.map(mapping => mapping.target)).size !== mappings.length
  ? 'Each source namespace must use a different target namespace.' : '';
}
export function drillScopeError(mode:string,excluded:string[],available:string[],loaded:boolean,loading:boolean,error:string) {
 if(loading) return 'Wait for restore point inspection to complete.';
 if(error) return 'Restore point inspection failed. Retry before continuing.';
 if(mode==='all')return '';
 if(!loaded)return 'Inspect restore point contents before using Custom scope.';
 if(available.length && available.every(key=>excluded.includes(key)))return 'At least one resource type must remain included in the restore.';
 return '';
}
