# Agent

Cluster-side agent source and delivery assets.

- `comm-agent/`: HyperCDR communication client. It handles registration,
  heartbeat, inventory collection, task receiving, status reporting, and Velero
  CRD operations.
- `installer/`: Cluster onboarding installer scripts and Kubernetes manifests.
- `../charts/hypercdr-agent/`: reserved location for a future supported agent
  Helm chart. Generated registration installers remain the canonical delivery path.

Velero is installed by the agent into the configured namespace in the target
Kubernetes cluster. Its pinned source is vendored under `../third_party/velero/`;
the backend also embeds the CRDs required by the generated installer.
