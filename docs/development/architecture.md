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
plan or cluster. Resource detail routes select a tenant guard at registration;
unknown API domains must declare their policy before they can mount. Collection,
batch, upload-owner, migration-token and administrative handlers retain their
specific scope checks. This does not enable PostgreSQL RLS or make global
administrative repository reads implicitly tenant-scoped.

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
Scheduler/janitor workers stop before PostgreSQL closes. Agents reconnect to the
active API. The deployment still uses a single
active slot. Distributed request correlation must be designed before advertising multiple active replicas. A scheduler lock
alone cannot guarantee correctness across arbitrary database/network partitions.

## Frontend and API

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
Authentication request/success schemas use the same wire DTOs as the handlers,
with live-response coverage tests. Other endpoint payload schemas remain incomplete; this is not yet a
client-generation contract. Existing error codes and HTTP statuses are preserved;
JSON errors can also carry the request ID from the response header. English
remains the UI language; the header shows an indicator rather than a switcher
that falsely suggests additional supported languages. Complete localization needs a separately defined
translation scope and visual acceptance, and is not claimed by this refactor.

## Checks

`make verify` runs backend PostgreSQL tests, agent tests, all frontend test groups,
TypeScript/ESLint checks, Go vet/format checks and deployment contract checks.
Keep evidence, caches, binaries and browser output outside the source tree.
