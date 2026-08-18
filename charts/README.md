# Helm charts

- `hypercdr-platform/` deploys the Community control plane.
- `hypercdr-agent/` deploys the shared managed-cluster agent components.

Charts contain portable defaults only. Cluster credentials, private Registry
credentials, certificates, and environment-specific values must remain outside
the repository.
