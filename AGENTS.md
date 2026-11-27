# HyperCDR Engineering Guide

## GitHub release checklist

- Push commits and tags only to the `github` remote (`oneprolabs/hypercdr`).
  Never push to `origin` unless the user explicitly requests it.
- A request to submit code and tag includes pushing both to GitHub.
- Before creating a community release tag, update and commit `VERSION` so its
  exact contents match the intended `ce-vX.Y.Z` tag. Never tag first.
- Run `bash scripts/release/check-tag-version.sh ce-vX.Y.Z` before tagging.
  This verifies both the working copy and committed `HEAD` version.
- Use a new version after a failed published tag; do not move remote tags
  without explicit user approval.
- Check the GitHub Actions release run after pushing. A successful push does
  not prove the build or publication succeeded.

## Repository boundaries

- `backend`: control-plane API, scheduler, persistence, migrations, and upgrader.
- `frontend`: React control-plane UI.
- `agent/comm-agent`: managed-cluster communication and task execution.
- `deploy/online`: first-install delivery and installer UX.
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

- Persist timestamps in UTC and convert only at display boundaries. Every
  user-visible timestamp must use the product's selected user timezone through
  the shared date-time helpers, including task timelines and the timestamp in
  `RP-YYYY-MM-DD HH:mm:ss` restore-point labels. Never format visible dates
  directly with the browser timezone or reuse a display string for ordering.
- Derive UI state from persisted task and resource data.
- Enforce tenant scope in storage and API paths, not only in the UI.
- Keep user-facing pages consistent with shared components and design tokens.
- Preserve backward compatibility for installed agents during rolling upgrades.
- Velero image changes must atomically reconcile image-bound capability annotations
  and preserve EnableCSI. Use the managed runtime upgrade path; never use a bare
  image replacement. Verify both server and node-agent readiness and capability
  metadata before declaring an installation or upgrade successful.
