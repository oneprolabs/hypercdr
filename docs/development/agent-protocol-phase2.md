# Agent protocol consolidation: phase 2 contract foundation

## Scope and source of truth

The authoritative installed-wire models are
`backend/internal/protocol/messages.go`; the message catalog and validator are
`backend/internal/protocol/contract.go`. The standalone Agent module uses exact
source copies, checked by `scripts/sync-agent-protocol.py --check` in `make lint`
and therefore `make verify`. Update the authoritative sources and run
`python3 -B scripts/sync-agent-protocol.py` to synchronize them. Agent-specific
message construction and random IDs remain separate from the contract.

These are intentionally checked-in Go source files needed by independently built
API and Agent images, not runtime build artifacts. The catalog is machine-readable
through `Contracts()` and JSON serialization; its typed payload declarations are
compiled and tested in both standalone modules. Every declared wire message must
have exactly one catalog entry. Adding a message constant without defining its
direction, family, allowed kinds and result authority fails the contract tests.

## Authority and delivery

| Family | Current messages | Authority |
| --- | --- | --- |
| command | task dispatch, task cancel | Authorize execution or request cancellation; not proof of success |
| execution | task accepted, completed, failed | Accepted changes admission only; completed/failed are terminal result candidates |
| telemetry | task progress | Update presentation samples, never declare terminal success |
| observation | Velero event, inventory report | Update observed resource facts, not create backup results |
| query | inventory/log/content request, log/content response | Obtain information without authorizing backup/restore side effects |
| control | registration, heartbeat, receipt, message error | Maintain connection and message delivery, not business completion |

An inventory response remains an observation regardless of whether it was pushed
or requested. Its allowed kinds are both `event` and `response`. Message kind is
not a substitute for the semantic family or result authority.

The Agent outbox reads terminal-result authority from the same catalog. A receipt
still must match the original durable message ID, type and task/command context,
and must say persisted. Neither the catalog's terminal flag nor a receipt proves
platform business success: phase-1 acceptance and publication checks still apply.

## Receive validation

Both established WebSocket receive loops validate supported wire version, known
message type, sender direction, explicit kind, authenticated cluster binding and
typed object payload before invoking business handlers. An invalid message is
ignored without disconnecting a valid session or mutating the task/outbox.
Identity, tenant ownership, command ownership, required result measurements and
operation-specific business invariants remain mandatory at the acceptance/store
boundaries. Envelope validation does not replace those checks.

The V1 validator deliberately does not invent stricter historical requirements:
missing version/kind/cluster metadata and additive fields are accepted, while an
explicit contradictory value is rejected. An empty payload object can pass the
wire decoder but is not evidence of a valid business result. Existing legacy map
fields are not transformed into fully typed operation-specific V2 results here.
Registration authentication continues to use its separate handshake path.
Malformed JSON still terminates the receive loop; the ignore-and-continue rule
applies to well-formed messages rejected by contract validation.

## State and error boundaries

The target state dimensions must remain distinct when the native implementation
lands; phase 1 already separates successful backup execution from quota-dependent
business finalization, but does not implement the general attempt engine.

| Dimension | Meaning | Must not be inferred from |
| --- | --- | --- |
| Delivery | Persisted/duplicate/rejected receipt for an exact message | Task progress, UI timeline or a resource observation |
| Admission | An execution attempt is accepted or rejected | Successful socket write or message receipt |
| Execution | Authorized attempt runs and produces one immutable succeeded/failed/canceled result | Inventory completeness or English message text |
| Business | Platform validates prerequisites, finalizes side effects and publishes the outcome | Agent result alone when quota cleanup or rollout verification remains |

Existing backup codes retain these meanings:

| Code | Meaning | Handling |
| --- | --- | --- |
| `TASK_IDENTITY_MISMATCH` | Authenticated cluster/command does not own the target execution | Permanent rejection; no task mutation |
| `BACKUP_RESULT_CONFLICT` | Accepted result and new terminal input disagree | Permanent rejection; preserve the accepted result |
| `BACKUP_RESULT_INCOMPLETE` | Final measurements or required backup identity are not complete | Keep finalizing/retryable; no empty point publication |

Wire rejection is separate from these business-result codes. No generic
`failed` state should be invented simply because an invalid frame was received.
The V1 receive validator currently logs and ignores such frames; durable,
machine-readable V2 rejection receipts belong to the delivery implementation.

## Native-version migration decision

V1 compatibility is no longer a release requirement after the user's approval.
The existing V1 catalog is retained as an auditable description of the running
system, not as a commitment to maintain two protocols indefinitely. Do not change
the version string to make this implementation look like V2: it does not yet
carry attempt IDs, immutable result IDs or fencing tokens.

The native V2 envelope, message families, operation results, error categories and
state model remain specified in
`docs/protocols/agent-platform-protocol-v2-design.zh.md`. Before switching traffic,
the schema and implementation must enforce them together with durable delivery
and reconciliation. Platform and Agent rollout must be coordinated; a new-only
platform must reject unsupported peers explicitly and must not interpret legacy
frames as a new execution. No silent result-field fallback should be introduced
to preserve old Agents.

Error handling must distinguish wire/schema rejection, unauthorized scope,
execution rejection, delivery failure and business result failure. The existing
backup result conflict/incomplete codes retain their phase-1 meaning. This phase
does not claim a new general V2 error engine or attempt state machine.

## Evidence and boundaries

- Both modules run the same catalog completeness, positive/negative direction,
  legacy metadata, additive field, zero measurement and malformed-payload tests.
- Real WebSocket black-box tests send unsupported-version, forged-cluster and
  wrong-kind terminal messages, then a valid ordering barrier on the same
  connection. Invalid events cannot fail a platform task or acknowledge an Agent
  result. All six backup result/observation/inventory orderings remain covered.
- Full backend, Agent and frontend checks run via `make verify`; targeted race
  tests cover the changed contract and delivery boundaries.

Runtime evidence is outside the repository at
`/data/hypercdr-runtime/validation/protocol-phase2`. These checks use isolated
databases and simulated peers over real transports, not live-cluster business
rehearsals. No 149/47 deployment, backup, Drill or snapshot deletion is implied.

This increment completes the contract foundation, not the whole V2 migration.
Durable platform inbox/outbox, attempt fencing, execution-result queries and
cross-operation transition engines still require subsequent implementation and
fault-injection validation before switching platform/Agent traffic.
