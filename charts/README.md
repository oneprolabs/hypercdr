# Helm assets

`hypercdr-platform/` contains Kubernetes platform deployment assets. The current public installer and blue/green lifecycle use Docker Compose, not this chart.

`hypercdr-agent/` is a reserved chart directory, not a complete supported agent installation chart. Use the platform-generated cluster registration installer.

Credentials, certificates and runtime values belong outside the checkout. Do not infer support or validation from the presence of a chart directory.
