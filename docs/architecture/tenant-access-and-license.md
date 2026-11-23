# Organization roles and cluster licensing

## Authorization

Community members have one role. Administrator and Operator are immutable,
shared built-in definitions. Operator has every business permission, including
destructive business operations, but no organization, member, role or license
management permissions. Platform administration remains separate.

Administrators create organization-local business roles. Role names are
case-insensitively unique within their organization. Custom roles cannot grant
organization or platform administration. Roles assigned to members or pending
invitations cannot be deleted. Role changes affect subsequent requests, including
existing authenticated sessions, without interrupting running tasks.

The authoritative business catalog is `backend/pkg/accesscontrol/permissions.go`:

| Permission | Purpose |
| --- | --- |
| `resources.view` | Read business resources and task details |
| `cluster.register` | Generate installation tokens and register clusters |
| `cluster.manage` | Manage cluster settings, agents and caches |
| `cluster.unregister` | Unregister clusters |
| `sync.manage` | Configure protection and manage synchronization |
| `drill.execute` | Run and clean drills |
| `recovery.execute` | Restore and take over workloads |
| `backup.delete` | Delete restore points and backup data |
| `storage.manage` | Manage object storage repositories |
| `policy.manage` | Manage protection policies |
| `task.abort` | Force abort, also requiring the task's business permission |
| `diagnostics.manage` | Collect cluster diagnostics |

Removing a protection plan also requires `backup.delete`. Unregistering with
explicit backup deletion requires both unregister and backup deletion permission.
Retry, cancellation and cleanup permissions derive from the persisted task, not
a caller-supplied task type. Installation token consumption checks the creator's
current membership and registration permission.

## Entitlement

Each organization starts with one total free cluster. Commercial licenses specify
total capacity, not extra slots. Online, offline and unregistering clusters count
until actually deleted. Registration and license activation use the same
tenant-scoped transaction lock to prevent concurrent over-admission. Rejected
registration does not consume the installation token. Existing agents may reconnect
at capacity. A consumed installation token cannot register another cluster.

Expiry falls back to free capacity. Existing clusters are not disconnected or
deleted, and protection/recovery work is not stopped. New registrations are blocked
when capacity is exhausted. Payment and Stripe integration are outside this scope.

## Offline issuance

Keep the issuer's Ed25519 private key off the platform and out of source control.
Configure `HCDR_LICENSE_PUBLIC_KEYS` on the platform as a JSON object mapping key
IDs to standard-base64 Ed25519 public keys. Without trusted keys, free usage works
but commercial activation is unavailable.

Claims contain `version: 1`, `product: "hypercdr"`, UUID `id` and `tenantId`, positive
monotonically increasing `revision`, `maxClusters` between 2 and 1000000, and UTC
RFC3339 `issuedAt`, `validFrom`, `validUntil`. Obtain the organization ID from
Settings → License & Usage. Issue a higher revision for renewal or capacity changes.

On the separate issuer machine:

```sh
chmod 600 /secure/issuer.pem
cd backend
go run ./cmd/license-sign --claims /secure/claims.json \
  --private-key /secure/issuer.pem --key-id commercial-issuer \
  > /secure/customer-license.json
```

The key must be PKCS#8 PEM Ed25519, readable only by its owner. Administrators
import signed JSON in License & Usage. Signature, tenant, product, dates, capacity
and revision are validated server-side. The same signed license can be reimported
idempotently; old or conflicting revisions are rejected. Envelope whitespace is
canonicalized before storage. Do not include raw licenses or keys in audit logs.

## API and migration

- `GET/POST /api/v1/saas/roles`, `PATCH/DELETE /api/v1/saas/roles/{id}`.
- `GET/POST /api/v1/saas/license`: members read, administrators activate.
- Member/invitation assignment accepts a built-in ID or same-tenant custom UUID.

Migration 000051 preserves historical administrator/operator assignments. It does
not choose a sole administrator or silently downgrade existing administrators.
Back up the database before deployment. An old binary does not understand custom
role restrictions: never roll back binaries while leaving custom-role members
active. Restore the matching database backup or disable affected accounts first.

Validate cross-tenant access, session permission changes, concurrent registration,
license rejection and expiry in isolated organizations. Compilation is not a
substitute for deployment and actual browser verification.
