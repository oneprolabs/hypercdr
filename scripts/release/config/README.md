# Release registry profiles

Registry profiles contain non-secret build and publication settings. Select one
explicitly when publishing:

```bash
./scripts/release/release-all.sh vYYYYMMDD.N \
  --config ./scripts/release/config/aliyun-acr.conf
```

Do not put passwords, access keys, or tokens in these tracked files. For an
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
velero-plugin-for-aws
velero-plugin-for-microsoft-azure
velero-plugin-for-gcp
```

The customized `velero` repository is published by its dedicated build flow.
Create repositories before publishing when the registry does not permit
automatic repository creation.
