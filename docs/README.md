# HyperCDR Documentation

## Start here

1. [Architecture overview](architecture/overview.md)
2. [Database schema](database/schema.md)
3. [Agent design](agent/agent-design.md)
4. [Platform-agent messages](protocols/platform-agent-messages.md)
5. [DR task state machine](protocols/dr-task-state-machine.md)
6. [Deployment guide](deployment/deployment-guide.zh.md)
7. [Build, release, and installation flow](deployment/build-release-install.md)

## Sections

- `architecture/`: System boundaries and component responsibilities.
- `decisions/`: Architecture Decision Records (ADRs).
- `deployment/`: Installation, release, upgrade, and rollback procedures.
- `development/`: Maintained developer workflows and component build guidance.
- `operations/`: Logging, diagnostics, and operational practices.
- `protocols/`: REST/WebSocket contracts, messages, and task state machines.
- `testing/`: Maintained regression-test procedures.
- `database/`: Schema, migrations, and persistence rules.
- `agent/`: Cluster agent implementation and installation design.

Host-specific notes, test evidence, handoff records, and generated reports do
not belong in this repository. Store them under `hypercdr-runtime`. Protocol and
schema changes must update the related document and automated tests in the same
pull request.
