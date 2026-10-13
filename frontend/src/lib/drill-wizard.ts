export const drillSteps = [
 ['Restore Point','Backup to recover'],
 ['Target','Cluster and namespace'],
 ['Restore Options','Scope and mappings'],
 ['Review','Confirm drill settings'],
] as const;
export function drillNamespaceError(namespace:string) {
 return /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(namespace) && namespace.length<=63 ? '' : 'Namespace must be 1–63 lowercase letters, numbers or hyphens, and start and end with a letter or number.';
}
export function drillScopeError(mode:string,excluded:string[],available:string[],loaded:boolean,loading:boolean,error:string) {
 if(loading) return 'Wait for restore point inspection to complete.';
 if(error) return 'Restore point inspection failed. Retry before continuing.';
 if(mode==='all')return '';
 if(!loaded)return 'Inspect restore point contents before using Custom scope.';
 if(available.length && available.every(key=>excluded.includes(key)))return 'At least one resource type must remain included in the restore.';
 return '';
}
