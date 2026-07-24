# HyperCDR Development Mode

This mode builds the backend from source and runs the frontend with the Vite
development server and HMR. Vite terminates HTTPS/WSS and proxies to the
backend over HTTP, so frontend source changes appear without rebuilding an
image. By default it reuses the standard platform PostgreSQL container when
one is available, preserving the current users, tenants, clusters, and other
platform data. It is separate from the standard release and Bootstrap flow.

Runtime files are stored outside the source tree under `/data/hypercdr/.dev`.
Go/npm caches remain under `/data/hypercdr/.cache`.
The backend and frontend run as transient `hypercdr-dev-api` and
`hypercdr-dev-frontend` systemd services; no permanent unit files are installed.
The frontend build uses the real frontend source as its root. A Git-ignored
`platform/frontend/node_modules` symlink points to external dependencies under
`/data/hypercdr/.dev/frontend` and is removed by `stop-dev.sh`.

```bash
cd /data/hypercdr/hypercdr-platform/deployments/dev
./start-dev.sh
./status-dev.sh
./stop-dev.sh
```

The first start creates `/data/hypercdr/.dev/dev.conf` from
`dev.conf.example`. Edit the external file for host-specific settings.

Set `HCDR_GOOGLE_CLIENT_ID` and `HCDR_GOOGLE_CLIENT_SECRET` there to enable
Google sign-in. The OAuth client must allow
`https://<host>:3002/api/v1/auth/google/callback` as a redirect URI.

Password reset links are delivered through `HCDR_SMTP_*` in deployed
environments. Trusted local development may use
`HCDR_PASSWORD_RESET_REVEAL_TOKEN=true` to return the one-time token directly
to the reset form; never enable that option on a public deployment.

Development endpoints:

```text
Frontend:  https://192.168.8.149:3002
Backend:   http://127.0.0.1:18080 (internal development proxy target)
Agent WSS: wss://192.168.8.149:3002/ws/agent
```

Do not run development mode and the standard Compose control plane at the same
time because they use the same host ports.
