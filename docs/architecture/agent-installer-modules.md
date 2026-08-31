# Agent installer module architecture

HyperCDR exposes one user-facing `/install.sh` command. The control plane
assembles that response in memory from independently maintained installer
modules, so modularity does not add downloads or network round trips.

## Dependency direction

```text
/install.sh entry and argument parser
  -> registration scenario contract
  -> provider contract
      -> native-kubernetes provider
      -> huaweicloud-cce provider
  -> common preflight and resource installation
  -> readiness and rollback
```

Provider modules implement context selection, provider verification, platform
trust preparation, provider preflight and trust installation hooks. Core code
must not contain a cloud vendor name. The Native provider must not call CCE
kubeconfig discovery, identity checks, dynamic PVC checks, platform certificate
pinning or the CCE Worker-Pod connectivity check.

The registration entry point accepts only the `fresh-install` scenario.
Upgrade, Community-to-Enterprise handover, online unregister and offline
uninstall remain separate workflows with their own authorization, state
machine and rollback semantics. They must not be added as branches inside the
fresh registration implementation.

## Performance rules

- Token, Kubernetes API and provider identity checks fail fast before mutation.
- Independent component image pulls execute concurrently. Adding another
  component must not add another full pull timeout to registration.
- CCE dynamic storage and Worker-Pod connectivity checks never execute for a
  Native Kubernetes registration.
- Every polling operation has a fixed deadline and prints actionable evidence
  before failing.
- Explicit failures and unexpected command errors use the same rollback path.
- The UI begins connection monitoring only after the user copies the command.

## Extension checklist

Adding a provider requires a new module implementing every provider-contract
hook, provider-specific tests and real registration/unregister acceptance. It
must not modify another provider module. Adding a lifecycle scenario requires
a dedicated workflow when its authorization, mutation or rollback semantics
differ from fresh registration.

