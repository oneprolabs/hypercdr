---
title: User guide
description: "The complete path for a first use of HyperCDR, plus the rules for accounts, organizations and permissions."
---

# User guide

This section assumes the platform is installed and that a sign-in address and a cluster to protect are available. The goal is to complete one full flow: **register a cluster → prepare storage and policies → configure protection → a successful backup → verify recovery with a drill**.

## Reading order

<div class="hcdr-grid">
  <a class="hcdr-card" href="/en/docs/user-guide/first-use"><b>1. First use</b><span>Five stages from cluster registration to recovery verification, with acceptance criteria for each stage.</span></a>
  <a class="hcdr-card" href="/en/docs/user-guide/users-and-organizations"><b>2. Users, organizations and permissions</b><span>Account ownership, organization isolation, platform administrator boundaries and bound email rules.</span></a>
</div>

## Prerequisites

- The platform address is reachable, and the administrator password has been changed from the initial `admin` / `admin123`.
- A **test namespace** is available for validation; do not test in a business environment.
- A kubeconfig is ready, and the account behind it can create resources.
- The S3-compatible object storage address, bucket and credentials are ready.

## Recommended practice

Validate on a small scale before you expand the protection scope. Complete the full flow, recovery included, on the test namespace before you connect production applications. The number of protection configurations has no practical value until recovery has been verified.

## Platform not installed

Start with [Deployment](/en/docs/deployment/).
