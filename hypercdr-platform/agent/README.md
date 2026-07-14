# Agent

Cluster-side agent source and delivery assets.

- `comm-agent/`: HyperCDR communication client. It handles registration,
  heartbeat, inventory collection, task receiving, status reporting, and Velero
  CRD operations.
- `installer/`: Cluster onboarding installer scripts and Kubernetes manifests.
- `charts/`: Agent Helm chart assets.

Velero is installed by the agent into the configured namespace in the target
Kubernetes cluster. This platform repository no longer stores the full Velero
source tree; it only keeps Velero CRD assets and image references. The full
Velero source is maintained in the sibling repository `../../hypercdr-velero/`.
