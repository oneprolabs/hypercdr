# HyperCDR PR review context

Review pull requests as production infrastructure and application code.

- Prioritize data loss, authentication/authorization, secret exposure, unsafe
  shell execution, container privilege escalation, and deployment rollback
  failures.
- Check both Go modules (`backend`, `agent/comm-agent`) and the frontend.
- Changes to Docker Compose, release scripts, or GitHub Actions must preserve
  blue/green deployment, ACR image references, health checks, and rollback.
- Prefer focused changes and existing project patterns. Do not require tests
  for documentation-only changes, but require tests for behavior changes.
- The repository's required verification command is `make verify`.
