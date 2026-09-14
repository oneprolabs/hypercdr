# Release Center API Contract

The Release Center is an independent service. It is not a control-plane
database and the release publisher must not call a specific control-plane
instance.

## Endpoints

`POST /api/v1/releases` stages the complete, immutable `release-manifest.json`.
The response status is `awaiting_installer`. The endpoint is idempotent for an
identical JSON document regardless of key ordering. Modified releases return
HTTP 409. Local server file paths are not accepted.

`POST /api/v1/releases/{version}/installer` uploads a gzip tar archive as its
binary request body. The embedded manifest must exactly match the registered
manifest. Invalid archives, unsafe paths, and mismatched manifests are rejected.
An identical upload is idempotent; replacing an existing archive returns 409.
The maximum archive size is 64 MiB; the maximum manifest size is 1 MiB.

`GET /api/v1/catalog` returns published releases and compatibility metadata.
The response is `{ "items": [...] }`. Only releases with a verified installer
are listed. Each item includes `installer.sha256`, `installer.size`, and a
relative `installer.url`. Uploading metadata alone does not publish a release.

`GET /api/v1/releases/{version}` returns one immutable release manifest.

`GET /api/v1/releases/{version}/installer` downloads the verified installer.
Clients must compare its SHA256 with the catalog before using the archive.
All API endpoints require Bearer authentication; `/healthz` is unauthenticated.

## Platform synchronization

Each control plane configures `HCDR_RELEASE_CENTER_URL` and periodically pulls
`/api/v1/catalog` and upserts the releases into its local release table. Network
or HTTP failures are logged and do not delete existing records. HTTPS uses
system trust or the configured private CA. Catalog signatures are not currently
implemented; deploy the service behind a trusted TLS reverse proxy. The upgrade
page continues to read the control plane's local `/api/v1/platform/releases` API.

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
