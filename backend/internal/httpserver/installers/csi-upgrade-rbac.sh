#!/usr/bin/env bash
set -euo pipefail

namespace=hypercdr-agent
apply=false
kubectl_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace) namespace="${2:?namespace required}"; shift 2 ;;
    --kubeconfig|--context) kubectl_args+=("$1" "${2:?value required}"); shift 2 ;;
    --apply) apply=true; shift ;;
    -h|--help) echo 'Usage: upgrade-cluster-csi-rbac.sh [--kubeconfig FILE] [--context NAME] [--namespace NAME] [--apply]'; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ && ${#namespace} -le 63 ]] || { echo 'Invalid namespace' >&2; exit 2; }
for command in kubectl jq sha256sum; do command -v "$command" >/dev/null; done
role=hypercdr-agent
if [[ "$namespace" != hypercdr-agent ]]; then
  role="hypercdr-agent-$(printf '%s' "$namespace" | sha256sum | cut -c1-8)"
fi
current="$(kubectl "${kubectl_args[@]}" get clusterrole "$role" -o json)"
patch="$(jq -c '
  def required: [
    {apiGroups:["storage.k8s.io"],resources:["storageclasses"],verbs:["get","list","watch","create"]},
    {apiGroups:["storage.k8s.io"],resources:["csidrivers"],verbs:["get","list","watch"]},
    {apiGroups:["snapshot.storage.k8s.io"],resources:["volumesnapshotclasses"],verbs:["get","list","watch"]},
    {apiGroups:["snapshot.storage.k8s.io"],resources:["volumesnapshots"],verbs:["get","list","watch","update","delete"]}
  ];
  . as $role |
  [required[] | . as $need | select(all($need.verbs[]; . as $verb |
    any($role.rules[]; ((.resourceNames // []) | length == 0) and
      any(.apiGroups[]; . == "*" or . == $need.apiGroups[0]) and
      any(.resources[]; . == "*" or . == $need.resources[0]) and
      any(.verbs[]; . == "*" or . == $verb))) | not)] as $missing |
  if ($missing | length) == 0 then [] else
    [{op:"test",path:"/metadata/resourceVersion",value:.metadata.resourceVersion},
     {op:"add",path:"/rules",value:(.rules + $missing)}]
  end' <<<"$current")"
if [[ "$patch" == '[]' ]]; then echo "CSI permissions already current: $role"; exit 0; fi
dry_run=(--dry-run=server)
if [[ "$apply" == true ]]; then dry_run=(); fi
kubectl "${kubectl_args[@]}" patch clusterrole "$role" --type=json -p "$patch" "${dry_run[@]}"
if [[ "$apply" == false ]]; then echo 'Preview only. Run again with --apply using cluster administrator credentials.'; fi
