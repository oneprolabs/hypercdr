# Restored workload readiness visibility

The Waiting for Workloads stage reports Kubernetes readiness, not business-level
availability. Data restoration failures remain attributed to Restoring Persistent
Data. This change does not change success criteria or automatic failure policy.

The agent checks readiness every five seconds with a bounded Kubernetes request
context. Changed snapshots are sent immediately; unchanged snapshots are sent at
most every thirty seconds. The API persists the latest readiness snapshot, stage
start, last successful observation, task deadline, and observation error. Only
changed snapshots add `workload_readiness_changed` timeline events. Ordinary
transfer progress and older agents retain their existing behavior.

Snapshots include Pods, nonzero-replica Deployments/StatefulSets/DaemonSets, and
expected PVCs. Objects expose identity, current state, reason, message, replica
counts, node/container details, and the time their current state was first
observed. Object observations are sorted to avoid events caused by API list order.
The latest Kubernetes Warning for an unready object's UID supplies diagnostic
evidence without changing readiness decisions. Repeated warnings update their
last-seen time and occurrence count without creating another timeline transition.
State observation times restart after an agent process restart; they are not
claims about the original Kubernetes transition time.

The task list summarizes ready workloads and the current Pod blocker. Stage
details use the same individual timeline rows as other recovery stages: checking,
starting, waiting for probes, object ready, and explicit stage completion with
elapsed time. There is no separate readiness summary card or deadline block.
Object names and raw diagnostic messages are available through event details.
Normal waiting observations are neutral markers, not green success markers.
API read failures preserve the last
successful observation and explicitly indicate observation failure. They do not
claim that the namespace is missing. Timeouts retain the final readiness reason.
Active observations older than ninety seconds are labeled stale rather than
presented as live status.

`readiness.ImageEvents` is an additive observation field containing retained
Kubernetes Pulling/Pulled/Failed/BackOff events correlated by Pod UID and actual
container name. Each observation carries the actual target image reference,
Pod/node/container identity, first/last timestamps, and occurrence count. Locally
cached images are distinct from downloads. Velero's managed restore-wait helper
is excluded from application-startup image events. Missing events are not inferred
from a Running container. Old agents remain compatible but cannot supply image
history that they never observed.

The frontend derives compact child rows from persisted snapshots and task events.
Multiple containers/Pods remain distinct. Repeated image failures are aggregated;
their full image address is selectable and copyable, with detailed kubelet errors
behind an explicit disclosure. Successful pulls retain the address in details.
All rendered observation times use the selected product timezone. Copying an image
reference does not execute a pull or send a request to that registry.

Older tasks without structured observations keep the existing timeline fallback.
Agent, API, and frontend updates are all required for the full experience; frontend
or API updates alone do not instrument already-installed agents. No cluster agent
upgrade or platform deployment is performed by the source change itself.
