# HyperCDR diagnostic logging design

## Goals

The logging subsystem must identify where an operation failed, preserve the
lowest useful error cause, and correlate the platform, task, command and
cluster execution path without exposing another tenant's data or credentials.

## Data paths

Applications continue to emit JSON to stdout. Docker and Kubernetes own raw
container-log persistence and rotation. In addition, the platform stores a
bounded, structured diagnostic index in PostgreSQL for safe UI querying.

Task events are copied into the diagnostic index. HTTP mutations and failed API
requests are indexed with a request ID. Cluster component logs are collected on
demand through the authenticated comm-agent connection; arbitrary namespaces,
pods and containers are never accepted from the browser.

## Access model

* Tenant users can only query and export their own tenant and clusters.
* System administrators can select All tenants, one tenant, or System logs.
* System logs have no tenant ID and are only visible to system administrators.
* Authorization is enforced by the API and repeated for exports.

## Required correlation fields

`tenant_id`, `cluster_id`, `task_id`, `command_id`, `request_id`, `operation`,
`component`, `status`, `duration_ms`, `error_code` and `timestamp` are carried
whenever the information exists. Time is stored in UTC and converted by the UI.

## Safety and limits

Passwords, bearer tokens, agent credentials, object-storage secrets, SMTP
passwords and Kubernetes Secret content are never stored. Diagnostic payloads
are recursively redacted. Queries are time bounded and return at most 5,000
records. Exports apply the same authorization and filtering as the list API.

## Operational retention

The structured diagnostic index defaults to 14 days. Raw Docker logs use the
local driver with 50 MB per file and five files per service. Kubernetes raw-log
retention remains a kubelet/runtime responsibility. A future Loki backend can
replace raw-log retrieval without changing the API or UI authorization model.
