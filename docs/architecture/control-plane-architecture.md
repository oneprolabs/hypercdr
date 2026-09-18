# HyperCDR Control Plane Architecture

## 1. Scope

This document describes the current HyperCDR control-plane architecture as implemented in the repository and Docker Compose deployment. It describes the runtime design, not future blue-green or message-broker-based designs.

## 2. Architectural summary

HyperCDR uses a control-plane-centric architecture. The control plane exposes the web UI and HTTP API, persists durable state in PostgreSQL, and maintains WebSocket connections to cluster Agents. Agents execute operations inside Kubernetes, OpenShift, or CCE clusters and report inventory, task progress, and events back to the control plane.

The current deployment does not require Redis or an external message broker. PostgreSQL is the durable source of truth for tasks; WebSocket is the real-time transport; in-process Go channels and goroutines provide local coordination.

## 2.1 Technology stack

| Layer | Technology | Role |
|---|---|---|
| Web UI | React and TypeScript | Control-plane management interface |
| HTTP backend | Go `net/http` | REST APIs, authentication, orchestration, and scheduling |
| WebSocket | Gorilla WebSocket | Bidirectional Agent registration, task, progress, and event transport |
| Database | PostgreSQL | Durable users, clusters, tasks, plans, schedules, events, and configuration |
| Cluster execution | Kubernetes API | Agent-side operations on Kubernetes, OpenShift, and CCE |
| Backup and recovery | Velero/OADP and CSI integrations | Backup, restore, synchronization, and DR workflows |
| Deployment | Docker Compose | Control-plane container lifecycle and private service networking |
| Security transport | TLS/HTTPS/WSS | Browser and Agent secure communication |
| Login challenge | Cloudflare Turnstile (optional) | Human-verification challenge during authentication |
| Registration helper | `cluster-registration-executor` | Restricted cluster registration workflow execution |
| Platform lifecycle | `platform-upgrader` | Upgrade and deployment lifecycle operations |

The backend does not use Gin, Echo, Fiber, or another Go Web framework. It uses the Go standard HTTP library with application-owned routing and middleware. The platform also does not currently use Redis, RabbitMQ, Kafka, NATS, or another external message broker.

```text
                    Users / Operators
                           |
                    HTTPS (external port)
                           |
             +-------------v--------------+
             | Platform Frontend          |
             | TLS termination / web UI   |
             +-------------+--------------+
                           |
                    HTTP internal API
                           |
             +-------------v--------------+
             | Platform API               |
             | REST, scheduler, auth,     |
             | task orchestration, WS hub |
             +------+------+--------------+
                    |      |
             SQL    |      | HTTP internal calls
                    |      +--> Registration Executor
             +------v---+  +--> Platform Upgrader
             | PostgreSQL|
             | durable   |
             | state     |
             +-----------+

             WebSocket over configured public endpoint
             +----------------------------------------+
             | Kubernetes/OpenShift/CCE Agent         |
             | task execution, inventory, DR events   |
             +----------------------------------------+
```

## 3. Runtime components

### 3.1 Platform frontend

`hypercdr-platform-frontend` serves the React web application and terminates the externally exposed TLS connection. The host-facing port is configurable (default `12443`) and maps to the container's port `3002`. The frontend proxies or targets the API using the configured platform URLs.

### 3.2 Platform API

`hypercdr-platform-api` is the main backend service. Its responsibilities include:

- REST APIs for authentication, cluster management, storage, plans, schedules, backup, sync, and drill workflows.
- WebSocket endpoint for Agent registration, heartbeats, task dispatch, progress, and event reporting.
- Task creation, state transitions, retries, and redispatch after Agent reconnect.
- Scheduler execution for configured schedules.
- Registration-token validation and cluster admission.
- Release and platform configuration integration.
- Cloudflare Turnstile verification when challenge mode is enabled.

The API listens internally on `18080`. It connects to PostgreSQL and calls the registration executor and upgrader through internal container networking.

### 3.3 PostgreSQL

`hypercdr-postgres` stores durable control-plane state, including users, clusters, Agent credentials, tasks, task events, plans, schedules, storage resources, and migration/recovery records. The database volume is mounted below the configured installation directory.

PostgreSQL is the authoritative source for task state. A task normally moves through states such as `queued`, `dispatched`, progress updates, and terminal `succeeded` or `failed` states.

### 3.4 Cluster registration executor

`hypercdr-cluster-registration-executor` is a restricted helper service used for cluster registration workflows. It has no general-purpose message-queue role. It uses the shared registration-session volume and communicates with the API over an authenticated internal HTTP endpoint.

The container runs read-only, drops Linux capabilities, uses a small `tmpfs`, and is isolated from direct external exposure.

### 3.5 Platform upgrader

`hypercdr-platform-upgrader` performs platform upgrade operations using the deployment directory and Docker socket. It is an operational helper rather than part of normal Agent task dispatch. Its lifecycle and availability depend on the selected release/deployment mode.

### 3.6 Cluster Agent

An Agent runs inside each managed cluster. It uses the Kubernetes API and cluster-native resources to execute Velero/OADP and DR operations. The Agent connects outbound to the API's WebSocket endpoint, registers with an install token or existing Agent credential, sends heartbeats and inventory, receives task commands, and reports progress and events.

The registration command is generated from the configured public base URL and WebSocket endpoint. It must not assume the default port when a custom platform port is configured.

## 4. Communication paths

| Path | Transport | Purpose |
|---|---|---|
| Browser -> frontend | HTTPS | UI access and authentication |
| Frontend -> API | Internal HTTP/API proxy | UI data and actions |
| API -> PostgreSQL | PostgreSQL protocol | Durable state and transactions |
| API -> registration executor | Internal HTTP | Registration helper operations |
| API -> upgrader | Internal HTTP | Platform lifecycle operations |
| Agent -> API | WebSocket over TLS | Registration, heartbeats, task dispatch, progress, and events |
| Agent -> Kubernetes/OpenShift/CCE API | Kubernetes API | Cluster-side execution |

No inbound connection from the control plane to a cluster Agent is required for normal task delivery; the Agent maintains the outbound WebSocket connection.

## 5. Agent registration and connection lifecycle

1. The operator obtains a generated installation command from the API.
2. The Agent installer validates the token against the actual configured control-plane URL.
3. The Agent connects to the configured WebSocket endpoint.
4. The Agent sends an `agent.register` message.
5. The API validates the token or Agent credential and persists/updates the cluster record.
6. The API returns registration acceptance and an Agent credential.
7. The Agent starts heartbeats, inventory reporting, and event reporting.
8. When the connection is restored, the API scans PostgreSQL for `queued` or `dispatched` tasks and redispatches eligible tasks.

The externally advertised base URL, public base URL, API URL, and Agent WebSocket endpoint are configuration-derived. They must remain consistent when a non-default host port is used.

## 6. Task execution model

Task creation and dispatch are separate concerns:

1. An API or scheduler operation creates a durable task in PostgreSQL.
2. The task is initially `queued`.
3. The API finds the Agent connection for the target cluster.
4. The task is sent over WebSocket and marked `dispatched`.
5. The Agent reports progress and task events, identified by the task ID.
6. The API updates the durable task record and exposes the result to the UI.
7. Terminal tasks are not redispatched.

If an Agent reconnects, the API reads pending tasks from PostgreSQL and attempts redispatch. This provides recovery without Redis, but requires careful idempotency and state-transition handling for scale-out deployments.

## 7. Scheduling model

Schedules are stored and evaluated by the control-plane scheduler. A schedule produces a normal one-time task; the cluster does not independently create periodic control-plane tasks. The scheduler therefore remains the authority for recurring backup or DR operations.

## 8. Persistence and recovery

Persistent data is divided into:

- PostgreSQL data: users, configuration, cluster identity, task state, events, and workflow records.
- Installation/deployment directory: Compose files, generated environment configuration, TLS material, and deployment metadata.
- Registration-session volume: temporary or workflow-specific registration session data.
- Cluster-side resources: Agent deployment, secrets, Velero/OADP resources, and application data managed by the cluster.

Restoring the control plane requires both the database state and the deployment configuration to be consistent. Restoring only containers without PostgreSQL does not restore task or cluster identity.

## 9. Security boundaries

- External UI/API access is protected by TLS.
- User authentication is handled by the platform API; Turnstile can be used as an authentication challenge.
- Registration tokens are short-lived/one-time bootstrap credentials; long-term Agent communication uses the issued Agent credential.
- Internal helper services use dedicated internal endpoints and tokens.
- PostgreSQL and helper services are not intended to be exposed directly to users.
- The registration executor is hardened with read-only filesystem settings, dropped capabilities, and no external port.

## 10. Current limitations and scaling considerations

The current design is appropriate for a single control-plane instance and moderate cluster/task volume. It has these scaling boundaries:

- WebSocket connection ownership is process-local.
- PostgreSQL queries provide pending-task recovery and can become a contention point at high volume.
- There is no distributed broker for cross-instance task routing.
- Progress-heavy workloads can create database writes and WebSocket traffic pressure.
- Multi-instance deployment requires shared connection routing, task leases/locking, and idempotent dispatch.

For a large-cluster deployment, the recommended evolution is PostgreSQL as the source of truth plus an Outbox-based dispatcher and a durable broker such as NATS JetStream or RabbitMQ. WebSocket gateways and dispatch workers can then scale independently. The broker should transport commands/events, not replace the authoritative task records in PostgreSQL.

## 11. Deployment topology

The standard Docker Compose topology contains:

- `hypercdr-postgres`
- `hypercdr-platform-api`
- `hypercdr-platform-frontend`
- `hypercdr-platform-upgrader`
- `hypercdr-cluster-registration-executor`

The frontend exposes the configurable external port (default `12443`), while the API, PostgreSQL, upgrader, and registration executor communicate through the private Compose network unless explicitly configured otherwise.

## 12. Source references

- `docker-compose.yml`
- `backend/internal/httpserver/agent_connection.go`
- `backend/internal/httpserver/task_dispatch.go`
- `backend/internal/store/postgres.go`
- `docs/protocols/platform-agent-messages.md`
- `docs/protocols/dr-task-state-machine.md`
- `docs/agent/agent-design.md`
