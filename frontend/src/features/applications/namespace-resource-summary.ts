import type { ResourceCategory } from '../clusters/types.ts';

export function mergeNamespaceResourceCategories(members: { namespace: string; categories: ResourceCategory[] }[]): ResourceCategory[] {
  const merged: ResourceCategory[] = [];
  for (const member of members) {
    for (const category of member.categories) {
      let target = merged.find(item => item.key === category.key);
      if (!target) {
        target = { ...category, total: 0, items: [] };
        merged.push(target);
      }
      target.total += category.total;
      for (const item of category.items || []) {
        const resources = item.resources?.map(resource => ({ ...resource, namespace: resource.namespace || member.namespace }));
        const existing = target.items!.find(candidate => candidate.kind === item.kind);
        if (!existing) target.items!.push({ ...item, resources });
        else {
          existing.count += item.count;
          if (resources) existing.resources = [...(existing.resources || []), ...resources];
        }
      }
    }
  }
  return merged.filter(category => category.total > 0);
}
