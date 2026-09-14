# Release Center

This independent file-backed service accepts immutable release manifests and
provides catalog, manifest, and installer endpoints. `RELEASE_CENTER_TOKEN` is
required. Mount `RELEASE_CENTER_DATA` as persistent storage. This service has no
browser management UI; Bootstrap Portal is a separate installer distribution UI.

The publisher first registers a manifest and then uploads the archive over HTTP.
Build servers and Release Center do not need shared filesystem access. Releases
appear in the catalog only after the archive and embedded manifest are validated.
Use HTTPS termination in front of this service outside an isolated test network.

See [API contract](../../docs/release-center-api.md) for requests and configuration.

Run isolated HTTP integration tests from the repository root:

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s bootstrap/release-center -p 'test_*.py' -v
```
