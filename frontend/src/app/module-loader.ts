export function createModuleLoader<Value>(loader: () => Promise<Value>) {
  let pending: Promise<Value> | undefined;
  let ready = false;
  let resolved: Value | undefined;
  const load = () => {
    pending ??= Promise.resolve().then(loader).then(value => {
      ready = true;
      resolved = value;
      return value;
    }).catch(error => {
      pending = undefined;
      throw error;
    });
    return pending;
  };
  return { load, isReady: () => ready, getValue: () => resolved };
}

export async function warmPageModules(loaders: Array<() => Promise<unknown>>, isCurrent: () => boolean) {
  for (const load of loaders) {
    if (!isCurrent()) return;
    try {
      await load();
    } catch {
      continue;
    }
  }
}
