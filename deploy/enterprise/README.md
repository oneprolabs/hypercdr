# Anonymous Enterprise installation

Enterprise source and builds stay in `oneprolabs/hypercdr-ee`. Only the installer
entry, installation archives, SHA256 files and release manifests are public in
`oneprolabs/hypercdr`. No GitHub account, token or registry login is required.
Docker with Compose, Python 3, curl, OpenSSL, tar and sha256sum must be installed.
Run as root on a supported Linux Docker host (currently linux/amd64):

```bash
set -o pipefail
curl -q -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/enterprise/install.sh \
  | bash -s -- --base-url https://YOUR_HOST:12447 --yes
```

Use `--version v1.0.117.20261014-ent.2` to pin a release. Use `--install-dir`
to choose a new directory (default `/var/lib/hypercdr-enterprise`). Existing
installations are rejected; use the packaged upgrade script for upgrades.
The HTTPS origin must be reachable by users and agents. The generated certificate
is self-signed; replace it with a trusted certificate for production.

Enterprise release tags end in `-ent.N`; they never trigger Community builds or
deployment. Enterprise public releases are marked `latest=false`, preserving the
Community `/releases/latest` installer. Enterprise resolves only releases with
its own archive and checksum. SHA256 and edition/version identity are checked
before installation. Public downloads confer no Enterprise license.

## Automated publication

The private Enterprise release workflow publishes its verified installation
assets to the public Community repository after packaging tests pass. Configure
`PUBLIC_RELEASE_TOKEN` in the **Enterprise** repository: a fine-grained GitHub
credential limited to `oneprolabs/hypercdr`, with Contents read/write. Do not use
a registry password or a token with access to private Enterprise source.
Missing credentials fail public publication explicitly; private builds do not
silently claim public installation is available. Never attach source archives,
credentials, databases or local environment files as release assets.
