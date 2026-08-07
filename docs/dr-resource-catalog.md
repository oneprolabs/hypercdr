# DR Resource Catalog

HyperCDR discovers the resource types currently used by a single namespace
when custom backup selection is needed. The catalog is collected by Comm Agent
through Kubernetes discovery and persisted in cluster metadata as
`namespaceAPIs`.

## Loading behavior

- Moving one namespace from Select Application to Setup DR starts a background
  capability scan without blocking navigation.
- Opening DR Configuration never waits for discovery.
- Custom selection reads the persisted catalog first. If no cached catalog is
  available, both scope panels open immediately with a loading state while an
  on-demand scan completes.
- Multi-namespace plans use the complete backup scope and do not request a
  custom resource catalog.

Capability updates are merged by cluster and namespace. Scanning one namespace
replaces only that namespace's cached entries, including clearing stale entries
when the scan is empty; catalogs for other namespaces remain available.

Comm Agent uses a bounded eight-worker discovery scan. Its Kubernetes client is
configured for 50 QPS with a burst of 100 so clusters with many CRDs and
aggregated APIs do not incur client-side throttling while retaining bounded API
server load.
