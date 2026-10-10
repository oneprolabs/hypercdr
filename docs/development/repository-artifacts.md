# Repository artifacts and size policy

Source, reviewed product assets and required test fixtures belong in Git.
Dependency installations, renamed dependency backups, build output, temporary
files and runtime state belong in the sibling `hypercdr-runtime` directory.

`make verify` and the PR repository job enforce these rules using both the Git
index and existing tracked worktree files. Ignored files that are force-added
are still checked; deleting or replacing an indexed artifact does not hide it.
Any directory beginning with `node_modules` is ignored and rejected at any depth,
including `.before-*`, `.old` and `_backup` variants. Locally generated dependency
directories are rejected even before staging. Compiled outputs (native libraries,
executables, object/archive files, WebAssembly and bytecode) are rejected by
extension and common executable signatures. Symlink targets are not followed.

The default file-size limit is **1 MiB (1,048,576 bytes)**, applied to the Git
blob and the current tracked file. There are no large-file exceptions today.
Required fonts, images and test fixtures below the limit remain supported,
including the fonts currently used by the UI and Velero's retained test data.

If a genuinely required source asset exceeds this limit, add an individually
reviewed entry to `config/repository-assets.json` with its exact repository path,
SHA-256 digest, maximum byte size and a concrete reason. No wildcard exemptions
or entire-directory exemptions are supported. Updating the asset requires an
updated digest and review. Exceptions never permit dependency caches or compiled
outputs. An unused exception is rejected.

Example entry (replace every illustrative value before use):

```json
{
  "version": 1,
  "exceptions": [
    {
      "path": "frontend/src/assets/required-illustration.png",
      "sha256": "<actual 64-character lowercase SHA-256>",
      "maxBytes": 1200000,
      "reason": "Required product illustration; optimized alternatives were reviewed."
    }
  ]
}
```

Run the focused checks with:

```bash
python3 -B scripts/tests/repository-hygiene.py
bash scripts/tests/repository-hygiene.sh
```

## Velero source remains vendored

The complete currently retained `third_party/velero` tree, upstream baseline,
patches and build inputs remain in this repository. This change does not convert
Velero to a submodule or fetch its source at build time, prune its retained files,
or change `scripts/release/build-velero.sh`. The build still stages the retained
source outside the checkout and applies the existing Velero/Kopia patches.
Upstream Go source, CRDs, embedded files, required test data, build scripts,
licenses and integration metadata must not be removed merely to reduce size.

## Historical dependency backups

Ignoring or deleting a dependency directory in HEAD does not remove its Git
history. Any trial history cleanup must use an independent mirror with no
remotes and no shared object storage, limited to historical frontend dependency
backups. Keep the source repository and every existing branch/tag unchanged.

A production history rewrite is a separate decision: it changes affected commit
IDs and tag targets, may invalidate signatures and historical source references,
and requires coordination with collaborators. It must not remove Velero source
or be executed automatically by CI. Ordinary `git gc` cannot remove dependency
blobs that are still reachable through historical commits/tags.
