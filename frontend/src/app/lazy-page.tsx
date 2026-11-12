import React from 'react';
import { createModuleLoader } from './module-loader';

export function lazyWithUpgradeRecovery<Component extends React.ComponentType<any>>(loader: () => Promise<{ default: Component }>) {
  const module = createModuleLoader(loader);
  const component = React.lazy(async () => {
    const reloadKey = 'hypercdr.lazy-module-reload';
    try {
      const loaded = await module.load();
      window.sessionStorage.removeItem(reloadKey);
      return loaded;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const isStaleBuild = /dynamically imported module|importing a module script failed|failed to fetch/i.test(message);
      if (isStaleBuild && window.sessionStorage.getItem(reloadKey) !== '1') {
        window.sessionStorage.setItem(reloadKey, '1');
        window.location.reload();
        return new Promise<never>(() => undefined);
      }
      window.sessionStorage.removeItem(reloadKey);
      throw error;
    }
  });
  const Page = (props: React.ComponentProps<Component>) => {
    React.useEffect(() => {
      if (module.isReady()) window.sessionStorage.removeItem('hypercdr.lazy-module-reload');
    }, []);
    const loaded = module.getValue();
    return loaded ? React.createElement(loaded.default, props) : React.createElement(component, props);
  };
  return Object.assign(Page, { preload: module.load, isReady: module.isReady });
}
