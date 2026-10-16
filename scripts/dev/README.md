# HyperCDR Development Mode

This mode builds the backend and frontend from source and runs the frontend
with Vite preview. Vite terminates HTTPS/WSS and proxies to the backend over
HTTP. Restart development mode after changing frontend source. By default it
reuses the standard platform PostgreSQL container when
one is available, preserving the current users, tenants, clusters, and other
platform data. It is separate from the standard release flow.

Runtime files are stored outside the source tree under `../hypercdr-runtime/environments/community`.
Go/npm caches remain under `../hypercdr-runtime/cache`.
The backend and frontend run as transient `hypercdr-dev-api` and
`hypercdr-dev-frontend` systemd services; no permanent unit files are installed.
The frontend source is copied into the external runtime workspace before npm
dependencies and build output are generated. The source repository is not
modified by development startup.

```bash
cd /path/to/hypercdr
./scripts/dev/start-dev.sh
./scripts/dev/status-dev.sh
./scripts/dev/stop-dev.sh
```

The first start creates `../hypercdr-runtime/environments/community/dev.conf` from
`dev.conf.example`. Edit the external file for host-specific settings.

Set `HCDR_GOOGLE_CLIENT_ID` and `HCDR_GOOGLE_CLIENT_SECRET` there to enable
Google sign-in. The OAuth client must allow
`https://<development-domain>:3002/api/v1/saas/auth/google/callback` as a redirect URI.

Password reset links are delivered through `HCDR_SMTP_*` in deployed
environments. Trusted local development may use
`HCDR_PASSWORD_RESET_REVEAL_TOKEN=true` to return the one-time token directly
to the reset form; never enable that option on a public deployment.

Development endpoints:

```text
Frontend:  https://<reachable-host>:3002
Backend:   http://127.0.0.1:18080 (internal development proxy target)
Agent WSS: wss://<reachable-host>:3002/ws/agent
```

Development normally uses 3002 while production uses 12443. Check configured ports and database reuse before running both: different ports do not imply isolated data. Use source-compose for an independent database.

Google Web OAuth requires a registered domain or localhost redirect; an ordinary LAN IP is not accepted. Configure the default system SMTP for email verification and invitations. Community accounts belong to exactly one organization.
