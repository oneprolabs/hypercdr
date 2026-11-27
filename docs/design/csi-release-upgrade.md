# CSI release and existing-cluster upgrade

## Release identity

The native runtime release builds Velero from the checked-out HyperCDR source.
Its default image tag is `v1.18.2-hcdr.8-backup-volume-policy-v1-<source hash>-isolation-cache-v1`.
The hash covers Velero source, patches, build scripts and dependency policy;
new local source files participate before staging and have the same identity
after staging. The release manifest stores the registry's resolved image digest.
The image name carries the capability markers understood by both installation
and managed upgrades, including digest-pinned references and alternate registries.

The publication script will not reuse the old hcdr.7 dependency just because its
digest matches an old lock. It refuses a nonmatching tag override, refuses to
overwrite an existing source-addressed tag, and fails closed on registry errors.
An unchanged source-addressed image can be reused across platform releases.
GitHub still builds from its checkout: include all new source files and SQL
migrations in the eventual commit, not only modifications to tracked files.

## Platform and clusters are separate upgrades

1. Build and test the release, and deliver its complete release manifest with
   the API/frontend package. Platform deployment does not update every cluster.
2. Upgrade the communication Agent first, so it reports both Velero server and
   node-agent identities and understands the current managed upgrade protocol.
3. Reconcile legacy cluster permissions using an administrator's kubeconfig.
   The release package contains `upgrade-cluster-csi-rbac.sh`; the platform also
   serves the identical embedded script at `/upgrade-cluster-csi-rbac.sh`.

   ```bash
   bash upgrade-cluster-csi-rbac.sh --kubeconfig test-cluster.yaml
   bash upgrade-cluster-csi-rbac.sh --kubeconfig test-cluster.yaml --apply
   ```

   The first command is a server-side dry run. The second adds only missing
   StorageClass/CSI discovery and snapshot observation/cleanup permissions to
   the existing namespace-scoped HyperCDR ClusterRole. It preserves other rules,
   uses a resource-version precondition, and does not change credentials,
   workloads, business volumes, StorageClasses or snapshots. Use `--namespace`
   when the native Agent is installed in a nondefault namespace. Do not use this
   native-runtime script as an OpenShift/OADP migration procedure.

4. Use the platform's managed Velero upgrade. It updates both server and
   node-agent, keeps EnableCSI, config maps and image-bound capabilities, and
   checks readiness. A missing legacy permission fails before workload changes
   with administrator repair instructions; the Agent never escalates itself.
5. Verify CSI Sync and Drill on an isolated application, including the resolved
   manifest images, actual temporary PVC StorageClass, completed DataUpload and
   DataDownload, restored content and HyperCDR-owned temporary-resource cleanup.

## Preconditions and limits

The source still requires a compatible CSI storage driver, snapshot APIs and
controller, and a matching VolumeSnapshotClass. Upgrading HyperCDR does not
install arbitrary storage vendors' snapshot controllers or convert unsupported
business volumes. CSI here means crash consistency; no application stop/freeze
is introduced. The restore side needs the matching Velero/node-agent data-mover
support, but does not need source snapshots to remain after successful Sync.

Longhorn temporary single-replica policy has live V1 evidence. V2 adapter unit
tests do not constitute a validated V2 engine deployment. Unknown drivers retain
their source storage policy; this release does not promise universal single-
replica or copy-on-write optimization.
