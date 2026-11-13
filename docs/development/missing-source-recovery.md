# Recovery after a source namespace disappears

Inventory reconciliation retains an application's identity while it is referenced
by a protection plan, a plan membership, a restore point, or a task. When the
namespace is absent, its status is `Missing` and its current resource counters
and resource summary are cleared. Its last collection timestamp remains the
timestamp of the last actual observation. Cluster application counts continue
to describe the reported live inventory, not retained recovery identities.

An unreferenced absent application is removed. If a retained namespace reappears,
the inventory upsert keeps its application ID and protection configuration and
refreshes its current state. Existing restore points remain usable without
recreating a placeholder source namespace. New backups against a known missing
source are rejected without creating a backup task.

An inventory persistence error fails the matching inventory request but does not
terminate the Agent's WebSocket receive loop. Subsequent messages can still be
processed. No database schema or existing Agent wire protocol changes are needed.

The Agent records pending Restore submission separately from successful
submission. Recovery checks Kubernetes before resubmitting a pending Restore:
only a confirmed NotFound permits submission; an existing Restore resumes
observation, and an API read error leaves the pending record available for retry.
Already submitted or legacy records keep their existing observation behavior.
Terminal, aborted and cleanup-pending tasks are excluded from normal recovery.
