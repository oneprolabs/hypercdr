# Implementation Plan

This plan starts from the current repository state: project directories exist, but real platform and agent code has not been implemented yet. The running UI exists in the separate prototype project `hypercdr-hyperbdr-style`.

## Phase 0: Decisions and Foundations

Status: current phase.

Deliverables:

- Architecture overview.
- Database schema design.
- WebSocket protocol design.
- Agent design.
- Backend and agent technology decision.

Recommended decisions:

- Backend: Go service or Node/NestJS service. Go is recommended if the team wants protocol structs shared with the agent.
- Agent: Go.
- Database: PostgreSQL.
- Frontend: React/Vite, migrating from the prototype.
- Deployment: Docker Compose for local, Helm for production.

Exit criteria:

- Docs are committed.
- Backend scaffold can start without guessing table or protocol names.

## Phase 1: Platform Backend Skeleton

Deliverables:

- Backend service entrypoint.
- Config loading.
- Structured logging.
- Health endpoints.
- PostgreSQL connection.
- Migration runner.
- Base migrations for tenants, users, clusters, agent tokens, sessions.
- REST API skeleton.
- WebSocket endpoint skeleton.

Suggested endpoints:

- `GET /healthz`
- `GET /readyz`
- `POST /api/v1/agent-tokens`
- `GET /api/v1/clusters`
- `GET /api/v1/clusters/:id`
- `GET /ws/agent`

Exit criteria:

- Service starts locally.
- Health checks pass.
- Migrations run on empty PostgreSQL.
- Install token can be created.

## Phase 2: Agent Registration and Heartbeat

Deliverables:

- `comm-agent` Go module.
- WebSocket client.
- Registration message.
- Platform registration handler.
- Agent credential response.
- Heartbeat loop.
- Agent session tracking in database.
- Registration credential persistence so the one-time install token is not reused.

Exit criteria:

- Agent connects to platform.
- Platform creates cluster record.
- Platform marks cluster online.
- Cluster becomes offline after heartbeat timeout.
- Agent sends heartbeat summary after registration.

## Phase 3: Inventory Collection

Deliverables:

- Agent Kubernetes inventory collector.
- Platform inventory ingestion.
- Tables populated for cluster nodes and namespace applications.
- REST API returns cluster cards and application list.
- Full inventory is sent after registration and when the heartbeat inventory hash changes.

Exit criteria:

- A real cluster install results in UI/API showing node count, namespace count, app list, PVC counts, and Velero status.

## Phase 4: Frontend Migration

Deliverables:

- Move prototype UI into `platform/frontend`.
- Replace in-memory cluster, storage, policy, application data with REST API calls.
- Keep the current page model and flows.
- Add loading, empty, error, and offline states.

Exit criteria:

- Clusters page shows data from PostgreSQL.
- Applications page shows namespace inventory from agent reports.
- Add cluster modal creates a real install token and command.

## Phase 5: Storage, Policy, and Protection Plan

Deliverables:

- Storage repository CRUD.
- Policy CRUD.
- Protection plan CRUD.
- Protection wizard writes real records.
- Platform validates basic references.

Exit criteria:

- User can configure protection for one namespace app.
- Plan is persisted and visible after refresh.

## Phase 6: Backup Task Closed Loop

Deliverables:

- Scheduler creates backup tasks.
- Task engine dispatches backup commands.
- Agent creates Velero Backup CR.
- Agent watches Backup status.
- Platform records task events.
- Platform creates restore point on success.

Exit criteria:

- Manual backup creates a Velero Backup.
- Platform operation history shows progress and terminal state.
- Restore point appears after success.

## Phase 7: Drill Closed Loop

Deliverables:

- Restore point drill task creation.
- Agent creates Velero Restore CR with namespace mapping.
- Platform tracks drill progress.
- Drill result appears in operations.

Exit criteria:

- User can recover a namespace into a generated sandbox namespace.

## Phase 8: Takeover Closed Loop

Deliverables:

- Target cluster validation.
- Restore task dispatch to target cluster agent.
- Repository access validation.
- Velero Restore creation on target cluster.
- Takeover operation tracking.

Exit criteria:

- User can restore a selected restore point to a configured target cluster/namespace.

## Phase 9: Packaging and Operations

Deliverables:

- Platform Dockerfile.
- Local docker-compose with PostgreSQL.
- Platform Helm chart.
- Agent Helm chart.
- Installer script.
- Upgrade and unregister scripts.
- Basic observability.

Exit criteria:

- Platform can run locally with one command.
- Agent can be installed with generated command.
- Documentation covers install, upgrade, and uninstall.

## Work Order Recommendation

Start coding in this order:

1. Backend skeleton and migrations.
2. WebSocket registration path.
3. Agent skeleton.
4. Inventory collector.
5. Frontend migration for cluster/application pages.
6. Protection plan APIs.
7. Backup task loop.
8. Drill and takeover.

This order creates the earliest useful product loop: install agent, connect cluster, see real inventory in the platform.
