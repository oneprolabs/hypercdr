# Release registry profiles

Registry profiles are defined together in `config/registries.conf`. Change
`HCDR_ACTIVE_REGISTRY` there, or override it for one publication:

```bash
./scripts/release/release-all.sh vYYYYMMDD.N --registry-profile aliyun_acr
```

Do not put passwords, access keys, or tokens in the registry file. For an
interactive build host, authenticate once with `docker login <registry-host>`.
For CI, inject `HCDR_REGISTRY_USERNAME` and `HCDR_REGISTRY_PASSWORD` from the CI
secret store.

HyperCDR uses one repository per component beneath the configured namespace or
project. The current release flow requires:

```text
platform-api
platform-frontend
platform-upgrader
comm-agent
postgres
velero
velero-plugin-for-aws
velero-plugin-for-microsoft-azure
velero-plugin-for-gcp
```

The release flow mirrors PostgreSQL and builds the pinned customized Velero
source when its immutable target tag does not yet exist. Create repositories
before publishing when automatic repository creation is disabled.
