# Scripts

Project-level scripts.

## Build

- `build-backend.sh`: Builds `platform-api` and `platform-migrate`.
- `build-agent.sh`: Builds `comm-agent`.
- `build-frontend.sh`: Builds frontend static assets.
- `build-all.sh`: Builds the backend, agent, and frontend.
- `build-images.sh`: Builds the platform API image and agent image.
- `export-bootstrap-images.sh`: Exports image bundles from the company build
  environment for import into a customer Harbor registry.

## Packaging

- `package-release.sh`: Creates a full release package under `dist/`.
- `package-bootstrap-portal.sh`: Creates the development bootstrap download
  portal and bootstrap release package.
- `package-harbor-tools.sh`: Creates a Harbor tool package that can be copied
  to a new server.

## Startup

- `start-platform-api.sh`: Starts the platform API from `dist/bin/platform-api`.
- `start-platform-frontend-dev.sh`: Starts the frontend Vite development
  server.
- `serve-bootstrap-portal.sh`: Serves the static bootstrap download portal
  locally. The default port is `8080`.

Scripts do not bind to a specific machine IP, certificate path, database
password, or image registry. Deployment-specific values should be provided
through environment variables.
