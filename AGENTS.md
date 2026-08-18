# HyperCDR Engineering Guide

## Repository boundaries

- `backend`: control-plane API, scheduler, persistence, migrations, and upgrader.
- `frontend`: React control-plane UI.
- `agent/comm-agent`: managed-cluster communication and task execution.
- `bootstrap`: first-install delivery and installer UX.
- `charts`: Kubernetes deployment assets.
- `third_party/velero`: pinned third-party source; avoid unrelated edits.

Generated files and local runtime state belong in `../hypercdr-runtime`, never
inside the repository. This includes dependency caches, frontend build output,
browser screenshots, test reports, logs, certificates, databases, temporary
files, and release packages. Do not use `.gitignore` as a substitute for this
source/runtime boundary.

## Required checks

Run `make verify` for repository-wide changes. Run the affected component test
suite for focused changes. Database and protocol changes require new tests and
updated documentation.

## Design rules

- Persist timestamps in UTC and convert only at display boundaries.
- Derive UI state from persisted task and resource data.
- Enforce tenant scope in storage and API paths, not only in the UI.
- Keep user-facing pages consistent with shared components and design tokens.
- Preserve backward compatibility for installed agents during rolling upgrades.
