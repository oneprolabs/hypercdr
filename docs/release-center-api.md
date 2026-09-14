# Release Center API Contract

The Release Center is an independent service. It is not a control-plane
database and the release publisher must not call a specific control-plane
instance.

## Endpoints

`POST /api/v1/releases` registers an immutable release. The request contains
the complete `release-manifest.json`, installer URL, checksum, and optional
compatibility metadata. The endpoint is idempotent by version and rejects a
changed digest for an existing version.

`GET /api/v1/catalog` returns published releases and compatibility metadata.
The response includes a catalog version, generation time, and release entries.

`GET /api/v1/releases/{version}` returns one immutable release manifest.

`GET /api/v1/releases/{version}/installer` downloads the platform installer.

## Platform synchronization

Each control plane configures `HCDR_RELEASE_CENTER_URL` and periodically pulls
`/api/v1/catalog`. It validates TLS, the manifest schema, image digests, and
the catalog signature before storing a local snapshot. Synchronization is
idempotent and failure is recorded without removing the last successful
snapshot.

## Configuration

```bash
# Release Center endpoint used to publish and synchronize release metadata.
HCDR_RELEASE_CENTER_URL=
# Token file used by the release publisher.
HCDR_RELEASE_CENTER_TOKEN_FILE=
# CA certificate for a private Release Center.
HCDR_RELEASE_CENTER_CA_FILE=
# Catalog synchronization interval, in seconds.
HCDR_RELEASE_CENTER_SYNC_INTERVAL=3600
```

The OCI registry remains separate: images are pulled from the configured
registry, while metadata and installer archives are obtained from the Release
Center. If the Release Center is unreachable, an administrator may apply a
local installer package containing its manifest and checksum.
