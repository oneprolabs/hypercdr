import { recoveryApplicationImages } from './recovery-images.ts';

type Resource = {
  namespace?: string; kind: string; name: string; images?: string[]; storageClasses?: string[];
  servicePorts?: Array<{ name?: string; port: number; protocol?: string; nodePort?: number }>;
};

export function recoveryAdvancedOptions(contents: Resource[], namespaces: string[], qualifiedServices: boolean) {
  const selected = contents.filter(item => item.namespace && namespaces.includes(item.namespace));
  const services = new Map<string, { namespace: string; service: string; name?: string; port: number; protocol: string; nodePort?: number; key: string }>();
  for (const item of selected.filter(item => item.kind === 'Service')) {
    for (const port of item.servicePorts || []) {
      const protocol = port.protocol || 'TCP';
      const key = `${qualifiedServices ? `${item.namespace}/` : ''}${item.name}|${port.port}|${protocol}`;
      services.set(key, { ...port, protocol, namespace: item.namespace!, service: item.name, key });
    }
  }
  return {
    storageClasses: Array.from(new Set(selected.flatMap(item => item.storageClasses || []))).sort(),
    images: recoveryApplicationImages(selected),
    servicePorts: [...services.values()].sort((left, right) => left.namespace.localeCompare(right.namespace) || left.service.localeCompare(right.service) || left.port - right.port),
    persistentDataExpected: selected.some(item => ['PersistentVolumeClaim', 'PersistentVolume', 'VolumeSnapshot'].includes(item.kind)),
  };
}

export function selectedAdvancedMappings(options: ReturnType<typeof recoveryAdvancedOptions>, config: {
  storageClassMappings: Record<string, string>; imageMappings: Record<string, string>; serviceNodePortMappings: Record<string, number>;
}) {
  const pick = <Value,>(values: Record<string, Value>, keys: string[]) => Object.fromEntries(Object.entries(values || {}).filter(([key]) => keys.includes(key)));
  return {
    storageClassMappings: pick(config.storageClassMappings, options.storageClasses),
    imageMappings: pick(config.imageMappings, options.images),
    serviceNodePortMappings: pick(config.serviceNodePortMappings, options.servicePorts.map(port => port.key)),
    persistentDataExpected: options.persistentDataExpected,
  };
}
