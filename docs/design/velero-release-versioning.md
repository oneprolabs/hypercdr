# Velero release identity

`third_party/velero/HYPERCDR_VERSION` is the canonical binary and release-manifest
version, formatted as `v<upstream>-hcdr.<revision>` (initially `v1.18.2-hcdr.9`).
Increment the revision for changes to the shipped Velero runtime. Build hashes
must never be used to order releases.

Image tags retain a separate `-build.<source fingerprint>` suffix for immutable,
source-addressed dependency reuse, followed by the existing
`-backup-volume-policy-v1-isolation-cache-v1` capability markers for compatibility
with installed runtime capability checks. Neither suffix participates in version
ordering or the displayed version. The binary embeds the canonical version and
its existing Git SHA field independently. Release manifests publish the short
version, full image reference, and registry digest separately. Installations and
managed upgrades use that full reference; server and node-agent readiness and
image identity checks remain required.

The communication agent reports the short version for new build tags while
retaining the complete image reference. The platform compares only upstream
and HyperCDR revision numbers for both standard and legacy long versions.
Different images at the same revision remain eligible for reconciliation;
older revisions are not advertised as upgrades.
