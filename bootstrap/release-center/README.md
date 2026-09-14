# Release Center

This directory defines the independent Release Center contract. The service
stores immutable release manifests and installer archives, exposes a catalog,
and never writes to a control-plane database.

Required endpoints:

* `POST /api/v1/releases` — authenticated release registration.
* `GET /api/v1/catalog` — published release catalog.
* `GET /api/v1/releases/{version}` — immutable manifest.
* `GET /api/v1/releases/{version}/installer` — installer download.

The initial deployment may use the reference file-backed implementation. A
production deployment must mount persistent storage and configure TLS and a
Bearer token. OCI images remain in the configured image registry.
