# OpenAPI route contract

Authenticated `GET /api/v1/schema` returns an OpenAPI 3.1 document built from the
live route registrations, including method/path, path parameters, access scope,
bearer security, endpoint-specific release-token alternatives, migration-session
authorization, and the shared error envelope (`error`, optional `message` and
`requestId`). It includes edition extension routes only when they are mounted.

Authentication routes share typed wire DTOs with their handlers and expose
request fields and successful payloads, including the configured challenge mode.
Tests check mounted auth coverage and compare actual login/profile/config/logout
responses with the schemas.

Application and tag routes also share DTOs with their handlers and document
query fields, collection responses, batch tag replacement, and creation status.
Policy CRUD routes use their existing store input/output DTOs, hide server-owned
tenant input, and validate actual create/list/update/delete responses.
Storage contracts cover CRUD, draft/saved connection tests and synchronization.
Connection-test responses share DTOs with their handlers, credentials are
write-only inputs and never appear in repository output, and sync distinguishes
201 dispatched tasks from 202 queued responses. Isolated PostgreSQL tests compare
actual CRUD/probe/queued-sync responses against these schemas without external
object-storage dependencies.
Task contracts cover reads/events, nullable latest tasks, cancellation, recovery
retry, drill cleanup and backup/restore/drill/takeover creation. Backup documents
single-task versus task-array responses and boolean versus numeric reuse fields;
actual-response tests cover both, including first/reused action statuses.
Protection-plan lifecycle responses share typed DTOs with the handlers and retain
non-null empty arrays and optional activation/cleanup tasks. Domain tests also
verify foreign task actions return 404 without mutating persisted tasks.
Domain route coverage tests fail when one of these mounted routes lacks a
payload contract.

This is an incremental contract: other endpoint request bodies and successful
payloads are not yet comprehensively modeled. Do not generate a client from it yet.
The route/access inventory is generated at runtime rather than copied by hand.
Tests verify coverage and default tenant guards. Unknown API domains fail route
registration until an access policy is explicitly added.

Release-token eligibility is shared with the authentication middleware and tested
against every mounted operation. Migration source session routes document
`Authorization: Migration <sessionToken>` separately from platform Bearer auth.
