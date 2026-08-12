# DR Resource Catalog

HyperCDR discovers the resource types currently used by a single namespace
when custom backup selection is needed. The catalog is collected by Comm Agent
through Kubernetes discovery and persisted in cluster metadata as
`namespaceAPIs`.

## Loading behavior

- Moving one namespace from Select Application to Setup DR starts a background
  capability scan without blocking navigation.
- Opening DR Configuration never waits for discovery.
- Opening Custom uses the last successful catalog immediately and refreshes it
  in the background. A transient cluster API failure never changes Custom back
  to Default or discards the operator's selection.
- Multi-namespace plans use the complete backup scope and do not request a
  custom resource catalog.

Capability updates are merged by cluster and namespace. Scanning one namespace
replaces only that namespace's cached entries, including clearing stale entries
when the scan is empty; catalogs for other namespaces remain available.

Comm Agent uses a bounded eight-worker discovery scan. Its Kubernetes client is
configured for 50 QPS with a burst of 100 so clusters with many CRDs and
aggregated APIs do not incur client-side throttling while retaining bounded API
server load.

## Custom scope presentation

Custom is an advanced namespace resource-type selector. It lists only resource
types that currently have objects in the selected namespace, their object
counts, and a concise type classification. Every row is independently
selectable; the platform does not add cluster-scoped resource types to the
operator's explicit Custom selection.

Dependency discovery and target readiness are deliberately not presented as
backup-selection guarantees. Target environment validation belongs to the
Drill preflight workflow, while Custom answers only which namespace resource
types enter the backup or restore scope.

All discovered namespace types remain selected initially. Runtime or
platform-managed types such as Event and aggregated action APIs are visibly
classified so an operator may explicitly exclude them; they are never silently
removed based on API group or resource name.
