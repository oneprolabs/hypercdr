# HyperCDR Velero patch set

The source under `third_party/velero` remains the pinned upstream baseline.
HyperCDR applies the patches listed in `series` only to an external staging
copy during builds.

The patch set adds virtual-hosted S3 support to Velero's Kopia data path:

- `s3ForcePathStyle=true` maps to Kopia/MinIO `path` lookup.
- `s3ForcePathStyle=false` maps to Kopia/MinIO `dns` lookup.
- an omitted value preserves MinIO's `auto` behavior.

The change is provider-neutral and contains no Huawei-specific endpoint logic.
It tracks Kopia issue #3902 and pull request #4462. Remove it after the pinned
Velero/Kopia versions provide equivalent upstream support.

Run `deployments/prepare-patched-source.sh` to create a patched source tree.
The script verifies each patch with `git apply --check` and stops on drift.

The metadata-only caching patch returns a passthrough content cache when
business-data caching is disabled. This preserves the separate metadata cache
without constructing a content cache backed by nil storage.

The task-cache-statistics patch adds a context-scoped observer, rather than
reading repository-wide cumulative counters. Index statistics account for
successfully loaded index blobs, including existing memory-mapped disk indexes,
with each index accounted once per task. Restore statistics count logical file
bytes returned to the filesystem output, preserving cache origins through
compressed objects and indirect chunks and excluding directory metadata.
Observers are independent even when tasks share a persistent cache directory.
