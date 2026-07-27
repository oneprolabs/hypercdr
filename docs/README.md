# HyperCDR Documentation

## Start here

1. [Architecture overview](architecture/overview.md)
2. [Database schema](database/schema.md)
3. [Agent design](agent/agent-design.md)
4. [Platform-agent messages](protocols/platform-agent-messages.md)
5. [DR task state machine](protocols/dr-task-state-machine.md)
6. [Deployment guide](deployment/deployment-guide.zh.md)

## Sections

- `architecture/`: System boundaries and component responsibilities.
- `decisions/`: Architecture Decision Records (ADRs).
- `deployment/`: Installation, release, upgrade, and rollback procedures.
- `development/`: Historical context and environment-specific engineering notes.
- `operations/`: Logging, diagnostics, and operational practices.
- `protocols/`: REST/WebSocket contracts, messages, and task state machines.
- `testing/`: Acceptance and regression-test procedures.
- `database/`: Schema, migrations, and persistence rules.
- `agent/`: Cluster agent implementation and installation design.

Environment-specific notes must stay under `development/`; they are not product
installation documentation. Protocol and schema changes must update the related
document and automated tests in the same pull request.
