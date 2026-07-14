# Velero Dependency

HyperCDR Platform does not keep the full Velero source tree in this repository.

The platform consumes Velero through:

- Bundled CRDs under `platform/backend/internal/veleroassets/crds`.
- Runtime image `hypercdr/velero:v1.17.1-helperfix`.
- Runtime image `hypercdr/velero-plugin-for-aws:v1.13.0`.
- Image sync metadata under `deployments/harbor/image-list.txt`.

The full Velero source is maintained in the sibling repository:

```text
../hypercdr-velero/
```

When Velero CRDs are updated, copy the generated CRD YAML files into
`platform/backend/internal/veleroassets/crds` and verify the agent installer
still serves `/assets/velero/v1.17.1/crds.yaml`.
