# Platform

Control plane source directory.

- `frontend/`: Web UI.
- `backend/api/`: REST API.
- `backend/websocket/`: Agent WebSocket server.
- `backend/task-engine/`: Task state machine and orchestration.
- `backend/scheduler/`: Policy scheduling and scheduled backup triggering.
- `backend/migrations/`: PostgreSQL migration notes. Runtime migrations are
  embedded from `backend/internal/migrations/sql/`.
