# Cluster registration v2

This document defines the provider-isolated registration workflow for the
first Linux-only automation release. Native Kubernetes keeps its existing
control-plane command flow. Huawei Cloud CCE adds platform-direct registration
and retains command-based registration as a network-isolation fallback.

## User modes

### Platform-direct (CCE only)

The user uploads a kubeconfig over HTTPS. The control plane stores it only in a
0600 temporary file for the registration task, creates an isolated execution
container (or a Kubernetes Job for a Helm deployment), and destroys the file,
container, and temporary resources on success, failure, cancellation, or
timeout. The kubeconfig is never persisted in the database or task logs.

The task first performs read-only checks. A user must select a context when the
file contains more than one. The UI shows the detected CCE alias, cluster ID,
region, Kubernetes version, worker capacity, permissions, image access, and
compatible StorageClasses. A missing default StorageClass requires explicit
user confirmation of the provider recommendation before installation starts.

### Command-based

The page displays prerequisites and one versioned command. The command is run
by the user on a Linux host that can reach the CCE API. The existing installer
performs all checks and installation. Its token is short-lived and single-use.
Native Kubernetes continues to use this mode on the control-plane node.

## Provider contract

Each provider owns the following operations and may not add branches to another
provider's implementation:

* `DiscoverCredentials`
* `SelectContext`
* `DiscoverIdentity`
* `CheckVersion`
* `CheckPermissions`
* `CheckNetwork`
* `DiscoverStorage`
* `CheckCapacity`
* `Install`
* `Rollback`
* `Uninstall`

The CCE identity source is `kube-system/cluster-config.data.alias`; kubeconfig
context is only a fallback. Native identity remains the control-plane node.

## Gates and execution rules

Checks run in a fixed order with per-stage timeouts and finite retries. No
formal Kubernetes resources are created before all mandatory gates pass.
Warnings are explicit and require confirmation; hard failures stop the task.
The execution plan has a stable idempotency key so retry resumes or safely
rolls back instead of creating duplicate namespaces.

The image registry is tested by an actual pull. DNS, TCP, TLS, HTTP status,
authentication, missing image, and timeout errors remain distinct. Object
storage is deliberately not checked during registration; it is validated by
Storage Configuration.

## Security and cleanup

Uploaded kubeconfigs are bounded to 1 MiB, parsed as kubeconfig only, and
`exec`/external authentication plugins are rejected for platform-direct mode.
The execution environment uses a fixed command allowlist and resource/time
limits. Cleanup is ownership-aware and never removes pre-existing business
resources. Every CCE installation also publishes an idempotent offline
uninstaller in the managed namespace; normal unregister still remains an Agent
task and removes the uninstaller with the namespace.

## UI contract

The CCE drawer presents the two modes explicitly. Platform-direct uses an
upload/context step, a read-only detection result, a final registration
summary, and a single start action. Command-based shows prerequisites and the
command directly. Loading, warning, failure, retry, rollback, and success
states reserve stable layout space and use the existing right-side drawer and
task list patterns. Native Kubernetes rendering and interaction are unchanged.

## Compatibility matrix

Provider thresholds and supported Kubernetes versions are versioned data, not
hard-coded in the shared installer. The first release supports Linux amd64
execution hosts and Ubuntu/Debian/RHEL/CentOS/Rocky/AlmaLinux dependency
installation. `kubectl` is selected to match the target server's major/minor
version, using an embedded client first and a checksum-verified download only
as fallback.
