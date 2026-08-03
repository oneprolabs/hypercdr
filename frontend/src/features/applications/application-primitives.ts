import type { ResourceCategory, ResourceCategoryKey, ResourceRef } from '../clusters/types';
import { shortResourceKind } from '../recovery/task-ui';

export const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function fallbackResourceRefs(kind: string, count: number, namespace: string): ResourceRef[] {
  void kind;
  void count;
  void namespace;
  return [];
}

export function buildResourceCategory(key: ResourceCategoryKey, label: string, items: Array<{ kind: string; count: number }>, namespace: string): ResourceCategory {
  const visibleItems = items.filter(item => item.count > 0).map(item => ({
    kind: item.kind,
    shortName: shortResourceKind(item.kind),
    count: item.count,
    resources: fallbackResourceRefs(item.kind, item.count, namespace),
  }));
  return {
    key,
    label,
    total: visibleItems.reduce((sum, item) => sum + item.count, 0),
    items: visibleItems,
  };
}

export const formatTime = (hour: number, minute: number) => `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
