# Scripts

Current entry points:

- `dev/start-dev.sh`, `dev/status-dev.sh`, `dev/update-dev.sh`, `dev/stop-dev.sh`: host development with external runtime and provenance.
- `dev/source-compose.sh`: isolated source-container development.
- `test-backend.sh`: tests with real isolated PostgreSQL.
- `build-frontend.sh`: frontend checks and build in external runtime.
- `release/`: local build, package, installation, blue/green and lifecycle tools.
- `registry-login.sh`, `lib/registry-config.sh`: registry authentication and profile resolution.
- `tests/`: deployment, release and repository contracts.

Public installation uses `deploy/online/install.sh`, not a development script.
Generated outputs, runtime configuration and credentials belong outside the checkout.
See [documentation](../docs/README.md), [development](dev/README.md) and [release tools](release/README.md).
