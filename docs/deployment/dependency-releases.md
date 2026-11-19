# Dependency releases

Container layers belong in the registry, not Git. Git stores the OADP source
lock, the runtime source lock, build recipes, and an approved dependency
manifest. Platform versions do not change dependency identities.

## Publish and approve

Run **Publish Dependency Images** with the selected registry profile. This
workflow also runs on main when its declared dependency inputs change. It
publishes content-input-qualified tags and uploads `dependency-manifest.json`
as a workflow artifact. Existing tags are inspected without pulling image
layers. Authentication, network, malformed-response and timeout errors fail
publication; only an explicit manifest-unknown response permits a build.

Review the artifact and commit it as `packaging/dependencies/approved.json`
before creating a platform release tag. Alternatively, manually dispatch the
platform workflow with `dependency_run_id` pointing at the successful
dependency workflow. Input fingerprints must match the platform checkout;
changing only the platform VERSION or application code does not invalidate
the dependency manifest. Artifact retention is 90 days; committing the approved
manifest makes approvals durable and independent of artifact retention.

The first run is a migration/bootstrap build, not evidence of warm-cache
performance. It can still transfer layers and compile Velero. Later runs
inspect existing input-qualified tags and reuse them. `rebuild_id` selects a
new tag namespace for an intentional rebuild; it never overwrites an existing
input-qualified tag. Registry retention rules must protect approved digests.

## Platform consumption

The default platform workflow resolves the approved manifest, validates its
registry, platform, complete component set and input fingerprints, and verifies
each dependency at its immutable digest. Only the five platform-owned images
are pulled for platform release verification. Dependencies are not rebuilt or
republished. Missing approval is an error, not an automatic full rebuild.

`legacy_dependencies=true` is a manual bootstrap fallback to the previous
dependency publication path. It is not selected automatically and is not the
recommended long-term publication path. Existing ce-v tags still reference
their old workflow; rerunning an old tag does not pick up these changes.

## Local entry points

```bash
python3 -B scripts/release/publish-dependencies.py \
  --registry REGISTRY/PREFIX \
  --runtime /data/hypercdr-runtime/build/dependencies \
  --output /data/hypercdr-runtime/build/dependencies/dependency-manifest.json

HCDR_DEPENDENCY_MANIFEST=/path/to/approved.json \
  scripts/release/release-all.sh --config /path/to/release.conf
```

The runtime source lock starts from the previously published ACR mirrors:
plugin digests from ce-v0.0.43's release manifest and PostgreSQL's locally
recorded ACR RepoDigest. Validate source availability during bootstrap before
approving the new manifest. This avoids rediscovering mutable upstream tags
on every platform release.
