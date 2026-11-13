export function createTaskDetailLoader<Task, Events>(loadTask: (id: string) => Promise<Task>, loadEvents: (id: string) => Promise<Events>, now = Date.now) {
  function cached<Value>(load: (id: string) => Promise<Value>) {
    const entries = new Map<string, { promise: Promise<Value>; expires: number; value?: Value }>();
    const read = (id: string) => {
      const existing = entries.get(id);
      if (existing && existing.expires > now()) return existing.promise;
      const entry: { promise: Promise<Value>; expires: number; value?: Value } = { promise: Promise.resolve().then(() => load(id)), expires: Infinity, value: existing?.value };
      entries.delete(id);
      entries.set(id, entry);
      while (entries.size > 20) entries.delete(entries.keys().next().value!);
      entry.promise.then(value => { entry.value = value; entry.expires = now() + 1000; }, () => { if (entries.get(id) === entry) entries.delete(id); });
      return entry.promise;
    };
    return Object.assign(read, { peek: (id: string) => entries.get(id)?.value });
  }
  const task = cached(loadTask);
  const events = cached(loadEvents);
  return { task, events, prefetch: (id: string) => { void task(id).catch(() => undefined); void events(id).catch(() => undefined); } };
}
