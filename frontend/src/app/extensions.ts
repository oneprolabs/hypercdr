import type { ComponentType } from 'react';
import type { LucideIcon } from 'lucide-react';

export type ExtensionViewId = `extension:${string}`;

export interface HyperCDRFrontendModule {
  id: string;
  view: ExtensionViewId;
  navigation: {
    label: string;
    description: string;
    icon: LucideIcon;
    order: number;
    group: 'settings';
  };
  component: ComponentType;
}

export function validateFrontendModules(modules: HyperCDRFrontendModule[]): HyperCDRFrontendModule[] {
  const ids = new Set<string>();
  const views = new Set<string>();
  for (const module of modules) {
    if (ids.has(module.id)) throw new Error(`Duplicate HyperCDR module id: ${module.id}`);
    if (views.has(module.view)) throw new Error(`Duplicate HyperCDR module view: ${module.view}`);
    ids.add(module.id);
    views.add(module.view);
  }
  return [...modules].sort((left, right) => left.navigation.order - right.navigation.order);
}
