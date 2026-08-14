import type { ComponentType } from 'react';
import type { LucideIcon } from 'lucide-react';
import type { ApiLoginResponse } from '../auth/types';

export type ExtensionViewId = `extension:${string}`;

export type FrontendModuleContext = {
  currentUser: ApiLoginResponse['user'];
  clusters: Array<{ id: string; tenantId: string; name: string; connectionStatus: string }>;
  toast: (message: string) => void;
};

export type FrontendModuleVisibilityContext = {
  currentUser: ApiLoginResponse['user'];
  capabilities: Record<string, { enabled?: boolean }>;
};

export interface HyperCDRFrontendModule {
  id: string;
  view: ExtensionViewId;
  navigation: {
    label: string;
    description: string;
    icon: LucideIcon;
    order: number;
    group: 'settings' | 'operations';
  };
  component: ComponentType<FrontendModuleContext>;
  isVisible?: (context: FrontendModuleVisibilityContext) => boolean;
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
