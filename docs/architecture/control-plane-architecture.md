# HyperCDR current architecture

The implementation is a Go control plane, React frontend, PostgreSQL persistence, and outbound cluster agents. The production topology is defined in `docker-compose.yml`.

```text
Browser → HTTPS edge :12443 → active blue/green frontend and API
                                  API ↔ PostgreSQL
Cluster comm-agent → WSS edge → API → persisted tasks and inventory
Cluster comm-agent → Kubernetes API → Velero / node-agent → object storage
Registration executor → Kubernetes API (Install for me)
Host upgrade runner → platform upgrade API → blue/green deployment script
Optional website → independent HTTPS :12445
```

PostgreSQL stores identities, user preferences, clusters, inventories, policies, protection plans, tasks, restore points and release metadata. There is no MemoryStore fallback. Tests use isolated PostgreSQL databases with real migrations. No Redis or external message broker is required by this topology.

Agents normally initiate outbound WebSocket connections. Platform-direct registration is a separate operation: the registration executor uses the supplied kubeconfig and needs connectivity to the managed Kubernetes API. Command registration runs on a host with that connectivity instead.

An application is represented by a namespace. Protection configuration associates source applications, target cluster, object storage and policy; the UI supports the three-stage workflow. Inventory and task results are reported by agents and persisted by the API. Dashboard platform and selected-cluster scopes use resource and task records, rather than sample metrics.

The versioned GitHub Release manifest supplies component images and versions. Installation activates a local manifest; successful platform blue/green deployment activates the target manifest. Cluster registration and component upgrades use the active component catalog. The host upgrade runner is distinct from cluster-side executors.

Native Kubernetes and CCE use the community Velero path; OpenShift has a separate OADP path. Bundled Velero source under `third_party/velero/` is retained and used by image builds. Repository assets or an adaptation checklist do not by themselves certify every Kubernetes distribution, storage driver or version combination.

Source boundaries: `backend/internal/httpserver/`, `backend/internal/store/`, `backend/internal/migrations/sql/`, `agent/comm-agent/`, `frontend/src/`, `deploy/online/`, `scripts/release/`, `website/`.

See [database](../database/schema.md), [registration](cluster-registration-v2.md), [implementation boundaries](../development/architecture.md), and [installation](../deployment/deployment-guide.zh.md).
