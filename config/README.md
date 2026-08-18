# Configuration contracts

Checked-in files in this directory are credential-free configuration contracts
and examples. `registries.conf` describes named Registry endpoints and image
prefixes; authentication is supplied by Docker, CI, or Kubernetes pull secrets.

Real credentials and host-local overrides belong under
`../hypercdr-runtime/shared/config` or another access-controlled runtime path.
