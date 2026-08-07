# Release-note fragments

User-visible changes should add one YAML fragment to this directory. Prefer the
issue or pull-request number as the filename.

```yaml
type: feature
audience: user
en: Added namespace resource selection for DR protection.
zh-CN: 新增容灾保护的命名空间资源选择。
```

Supported types are `feature`, `improvement`, and `fix`. Audience is `user` or
`admin`. Both translations are required. Entries must be concise, user-facing,
and free of credentials, customer data, infrastructure details, or unnecessary
security details.

The frontend manifest at `frontend/src/generated/release-notes.json` is the
release artifact consumed by the product UI. Release builds override its
version and release date using the release build inputs. Update the manifest
from the approved fragments before publishing a platform release.
