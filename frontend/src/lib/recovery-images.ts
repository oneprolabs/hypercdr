type Resource = {kind:string;name:string;namespace?:string;images?:string[]};
function veleroComponentImage(image:string) {
 const ref=image.split('@')[0];const last=ref.slice(ref.lastIndexOf('/')+1);const [repository,tag='']=last.split(':');
 return repository==='velero' || (repository==='hypercdr' && /^velero-v\d/.test(tag));
}
/** Legacy catalogs lack container names: omit only tooling added to a Pod
 * whose backed-up owner template does not contain that image. */
export function recoveryApplicationImages(resources:Resource[]) {
 const retained=new Set<string>();
 for(const item of resources) {
  const owner=item.kind==='Pod' ? resources.find(r=>r.namespace===item.namespace && (r.kind==='ReplicaSet'||r.kind==='StatefulSet'||r.kind==='Job') && item.name.startsWith(r.name+'-')) : undefined;
  for(const image of item.images||[]) {
   if(owner && veleroComponentImage(image) && !(owner.images||[]).includes(image)) continue;
   retained.add(image);
  }
 }
 return [...retained].sort();
}
