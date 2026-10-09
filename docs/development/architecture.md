# Control-plane boundaries

The platform persists business data in PostgreSQL. There is no MemoryStore,
production fallback, or storage-type exception to HTTP authentication. Tests
create isolated real databases; see [platform.md](platform.md).

## Persistence and services

`internal/store.Store` composes domain repository capabilities. Identity, audit,
and diagnostic consumers accept only the operations they need. PostgreSQL email,
identity, policy, protection, restore-point, and task operations live in separate
files. This is one implementation, with the same persisted semantics as the API.

Reusable schedule calculation lives in `internal/service/scheduling`. HTTP and
scheduler consumers use the same evaluator. Additional business services should
be extracted when multiple consumers need the rule, rather than adding a
pass-through layer to every CRUD operation.

Protection-plan insertion verifies tenant ownership of source/target clusters,
storage and policy, and source-cluster ownership of application references inside
the transaction. Task creation rejects an explicit tenant inconsistent with its
plan or cluster. Application tag replacement validates and locks every tag in
the application tenant before deleting old bindings; foreign/missing references
fail atomically, while duplicate valid tags and clearing remain supported. Resource detail routes select a tenant guard at registration;
unknown API domains must declare their policy before they can mount. Collection,
batch, upload-owner, migration-token and administrative handlers retain their
specific scope checks. This does not enable PostgreSQL RLS or make global
administrative repository reads implicitly tenant-scoped.

Application, task, restore-point and cluster-upgrade-status queries bind every
authenticated actor to its tenant before applying database pagination or limits,
including system administrators browsing their tenant workspace. Filtering a
global page afterward could otherwise hide valid local records when another
tenant has newer tasks/points or earlier-sorting applications. Explicit global
diagnostic/admin views retain their separately checked scope semantics.

## Scheduling and blue/green deployment

All scheduler ticks acquire the same transaction-scoped PostgreSQL advisory lock
on a dedicated connection. Overlapping API processes cannot both fire a tick;
the next process can acquire the lock after rollback or connection loss. Queued
executor work keeps its existing `FOR UPDATE SKIP LOCKED` claim semantics.
No schema change or change to published image/build/deployment ordering is
required for this lock.

This is **not general active/active support**. Captcha/OAuth challenges are hashed, expiring, single-use PostgreSQL records.
`DELETE RETURNING` guarantees one consumer across independent API pools; invalid
answers also consume a captcha. These challenges survive single-active handoff.
Agent sockets, pending inventory/log/content requests and temporary upload
metadata remain process-bound. Inventory lookup explicitly asks the user to
refresh again if a request was lost. Log/content waiters abort with a retryable
503 during shutdown; successful content indices remain persisted. Uploads are
restricted to their tenant and uploader, and expired on-disk kubeconfigs are
removed by the janitor. After restart users must upload/inspect again; persisted
registration tasks and executor-owned files retain their existing semantics.
Scheduler/janitor, storage preflight, protection activation, log maintenance,
content indexing and deferred cleanup workers are admitted through a lifecycle
gate and drained before PostgreSQL closes. Storage waits, index slots and retry
delays observe shutdown. Shutdown cancellation leaves persisted data tasks queued
rather than reporting a storage failure. Agent reconnect rechecks queued backup
and recovery storage dependencies from persisted plans/restore points before
dispatch; storage-sync and data-task messages share the task write lock. Agents reconnect to the
active API. The deployment still uses a single
active slot. Distributed request correlation must be designed before advertising multiple active replicas. A scheduler lock
alone cannot guarantee correctness across arbitrary database/network partitions.

Unregister object-store cleanup and component upgrade dispatch also use the worker
admission gate. Cleanup cancellation preserves a queued task; reconnect resumes
queued or interrupted running cleanup from its persisted repository references.
The `unregisterPreflightCompleted` task payload marker is saved only after object
deletion and requested protection-relationship cleanup succeed. Dispatch refuses
an uninstall with an incomplete preflight, and later reconnects reuse the durable
completion marker. Every repository reference is checked for existence and tenant
ownership before deletion begins. Remote object deletion is not transactional;
interrupted partial deletion is retried using the same tenant/cluster prefix.

Agent WebSocket handlers participate in shutdown admission/draining, including
connections waiting for their initial registration. Shutdown closes their sockets
and waits for handlers and ping loops before returning, since HTTP shutdown alone
does not close hijacked connections. Inventory, content/log requests, task commands
and event acknowledgements share a serialized writer with a bounded write deadline.
Real PostgreSQL tests exercise cancellation, interrupted cleanup, persisted
completion, credential reconnect, and concurrent inventory/task/ack socket writes.
Log and backup-content response waiters are keyed by the authenticated agent's
cluster and request ID. Supplying another cluster ID in a report cannot complete
that cluster's pending request. Real socket tests deliberately inject foreign
responses before the legitimate owner responds and verify the saved content index
remains readable after the owner disconnects.

## Frontend and API

App's resource state, refresh admission, mapping commits, selection, and polling
live in `app/use-platform-resources.ts`. `app/platform-snapshots.ts` owns the
multi-endpoint snapshots and plan-pointer hydration; it checks the session scope
before launching requests and after each asynchronous dependency. Late partial
cluster, topology, or pointed-task responses cannot repopulate a replaced session.
Session resource reset occurs before paint, and an old refresh cannot clear the
new session's in-flight refresh. Resource setters exposed to child pages are
also session-bound, so delayed mutation callbacks cannot populate a replaced
workspace after their page has unmounted. Prototype suppression of persisted resource
updates has been removed. Snapshot tests exercise delayed responses, session
replacement, expired scopes, missing pointer targets and bounded task histories.

App/Application DR resource reads and mutations use shared domain API modules
for clusters, applications/tags, storage, policies, plans, tasks and restore points.
Pages use `src/api/client.ts` for JSON, multipart uploads, and binary downloads.
The client owns session headers, expiration checks, error codes and request
correlation. ESLint rejects bare `fetch` in UI modules. Existing page modules
remain lazy-loaded; password flows, cluster context and namespace-resource drawer
rendering are separate components without DOM/style/interaction changes. Platform
data mapping and lazy namespace-detail tab loading are separate modules as well.
The four-tab namespace detail drawer owns its rendering and presentation model,
while the parent retains protection/recovery actions. Inventory and namespace
catalog endpoints have typed domain API modules; request limits and existing
workflow semantics are preserved.

Authenticated `/api/v1/schema` exposes the live route, access and error inventory.
Account preference/profile callbacks are scoped to the submitting session, just
like shared resource updates. A delayed success, error or finally callback from
an old session cannot restore its browser credentials, change the new session's
theme/timezone or publish an unrelated toast/pending state. Session transitions
reset preference pending flags before paint. Browser acceptance delays an actual
appearance request through signout/relogin without saving a user preference.
Task creation checks all cluster/application/plan/restore-point references against
the resolved tenant. Restore-point creation checks its source, application, plan,
storage and backup-task references; attaching a restore point during a task status
update uses the same tenant guard. These checks hold row locks inside the write
transaction and reject invalid references before changing data or latest-task
pointers. Late status reports retain the existing terminal-task immutability rule.
Direct-registration request identity is persisted as tenant + uploader + request
key. Exact retries reuse the original task even after an API restart, independent
of task history length; changed settings or a second request for the same upload
return a conflict. PostgreSQL unique indexes enforce request/session identity.
The single active API serializes sealed-file publication and task creation; this
does not introduce multi-active API support. Active tasks protect their temporary
credentials from deletion and restart cleanup until the persisted deadline.
Expiry marks an unfinished task failed with instructions to upload/inspect again.
The credential lifetime, installer timeout and Kubernetes Job deadline share the
provider timeout rule, including the longer configurable OpenShift timeout.
Authentication request/success schemas use the same wire DTOs as the handlers,
with live-response coverage tests. Restore-point list, cached/live content, and
single/multi-cluster delete responses also have shared wire DTOs and actual-handler
checks, including pagination, duplicate IDs, foreign/missing batch references,
offline queues and in-progress conflicts. Registration upload/inspection/task/delete
routes (including the CCE aliases) and the three registration/handover token routes
now have payload contracts. Inspection wire types are shared with the isolated
executor. Token checks declare body-token authentication and purpose restrictions;
real PostgreSQL tests cover repeated validation, expiration and cross-purpose denial.
Other endpoint payload schemas remain incomplete; this is not yet a
client-generation contract. Existing error codes and HTTP statuses are preserved;
JSON errors can also carry the request ID from the response header. English
remains the UI language; the header shows an indicator rather than a switcher
that falsely suggests additional supported languages. Complete localization needs a separately defined
translation scope and visual acceptance, and is not claimed by this refactor.

## Checks

`make verify` runs backend PostgreSQL tests, agent tests, all frontend test groups,
TypeScript/ESLint checks, Go vet/format checks and deployment contract checks.
Keep evidence, caches, binaries and browser output outside the source tree.
