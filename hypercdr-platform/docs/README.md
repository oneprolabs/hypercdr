# Docs

This directory contains design documents for the real platform project. It does
not depend on the prototype directories.

- `architecture/`: Overall architecture, module boundaries, and business flows.
- `api/`: REST API and WebSocket protocol documentation.
- `database/`: PostgreSQL schema, migration design, and data state machines.
- `agent/`: Agent installation, registration, inventory collection, task
  execution, and Velero integration.

## Current Design Documents

- [Architecture Overview](architecture/overview.md): Platform, agent, Velero,
  task flow, and security boundaries.
- [Implementation Plan](architecture/implementation-plan.md): Phased plan for
  moving from the current skeleton into the real project.
- [Database Schema Design](database/schema.md): Phase 1 PostgreSQL schema and
  status enums.
- [Agent WebSocket Protocol](api/websocket-protocol.md): WebSocket message
  definitions between agents and the platform.
- [Agent Design](agent/agent-design.md): `comm-agent` modules, inventory scope,
  Velero CRD integration, and installer design.

## Suggested Reading Order

1. `architecture/overview.md`
2. `architecture/implementation-plan.md`
3. `database/schema.md`
4. `api/websocket-protocol.md`
5. `agent/agent-design.md`
