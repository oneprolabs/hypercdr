---
title: Documentation overview
description: English entry point for the HyperCDR documentation, organised by deployment, user guide, operation manual, administration and reference.
---

# HyperCDR documentation

HyperCDR is a container disaster recovery platform for Kubernetes and OpenShift. The control plane manages clusters, protection policies, restore points, DR drills, recovery tasks and component upgrades; cluster agents connect outbound to the platform and coordinate Velero or OpenShift OADP to perform the actual work.

This site is written against the Community edition `ce-v0.0.8`. See [GitHub Releases](https://github.com/oneprolabs/hypercdr/releases) for packages and version history.

::: warning English text is being translated

The Chinese documentation is complete. Every English page exists so that the two languages mirror each other page for page, but the body text is being translated section by section — a page that is not translated yet says so and links back to the Chinese original.

:::

## Find your entry point

<div class="hcdr-grid">
  <a class="hcdr-card" href="/en/docs/deployment/"><b>I need to deploy the platform</b><span>Prerequisites, running the installer, and adjusting the configuration until you have a working console.</span></a>
  <a class="hcdr-card" href="/en/docs/user-guide/"><b>This is my first time</b><span>Register a cluster, prepare storage, configure protection, and finish one verified recovery.</span></a>
  <a class="hcdr-card" href="/en/docs/manual/"><b>I am looking up a feature</b><span>Task-oriented pages by module: clusters, storage, policies, DR, tasks and drills.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/"><b>I maintain the platform</b><span>Start and stop, upgrades, blue-green rollback, restore cache and troubleshooting.</span></a>
</div>

## Documentation map

| Section | Audience | What it covers |
| --- | --- | --- |
| [Product overview](/en/docs/product/) | Technical evaluation | What the platform is, control plane architecture, core concepts |
| [Deployment](/en/docs/deployment/) | Deployment engineers | Prerequisites, online installation, configuration, access URL |
| [User guide](/en/docs/user-guide/) | First-time users | Full path from cluster registration to verified recovery, users and permissions |
| [Operation manual](/en/docs/manual/) | Daily operation | Dashboard, clusters, storage, policies, application DR, tasks, drills, agent removal |
| [Administration](/en/docs/operations/) | Platform operators | Platform operations and upgrades, blue-green deployment, restore cache, troubleshooting, release maintenance |
| [Reference](/en/docs/reference/) | Everyone | Glossary, support matrix, limitations and security, CCE and OpenShift specifics |

## Reading conventions

- Platform commands are run in the installation directory, `/var/lib/hypercdr` by default.
- The documentation follows the code, scripts and `env.example` in the repository; example values must be replaced with your own.
- Capabilities present in the code are not necessarily validated for every version, driver and topology. Check the [support and compatibility scope](/en/docs/reference/support-scope) before using them in production.
- Never put passwords, kubeconfig files, private keys, certificates or database dumps into documents, repositories or public issues.

::: tip Chinese and English

The Chinese documentation is the complete reference today. Use the language switcher in the navigation bar to open the same page in Chinese.

:::
