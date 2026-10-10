# Third Party

Third-party dependency notes and integration metadata.

- `velero/`: pinned Velero source used for HyperCDR's customized image build.
  Keep upstream provenance and the pinned version documented when updating it.

The currently retained Velero source tree, patches, upstream baseline and build
inputs remain vendored. Repository size checks do not authorize deleting this
source or rewriting its history. See
[repository artifact policy](../docs/development/repository-artifacts.md).
