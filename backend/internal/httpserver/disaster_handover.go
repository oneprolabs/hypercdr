package httpserver

import (
	"net/http"
	"strings"

	"hypercdr-platform/platform/backend/internal/store"
)

func (r *Router) validateDisasterHandoverToken(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Token) == "" {
		writeJSON(w, 400, map[string]any{"error": "token_required"})
		return
	}
	if err := r.store.ValidateDisasterHandoverToken(strings.TrimSpace(body.Token)); err != nil {
		status := http.StatusUnauthorized
		code := "token_invalid"
		if err == store.ErrTokenExpired {
			status, code = http.StatusGone, "token_expired"
		} else if err == store.ErrTokenUsed {
			status, code = http.StatusConflict, "token_used"
		}
		writeJSON(w, status, map[string]any{"error": code})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true, "purpose": "disaster-handover"})
}

func (r *Router) disasterHandoverScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(`#!/usr/bin/env bash
set -euo pipefail
TOKEN=""; ENDPOINT=""; MIGRATION_ID=""; ROLLBACK_DEADLINE=""; NAMESPACE="hypercdr-agent"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --token) TOKEN="$2"; shift 2;;
    --endpoint) ENDPOINT="$2"; shift 2;;
    --migration-id) MIGRATION_ID="$2"; shift 2;;
    --rollback-deadline) ROLLBACK_DEADLINE="$2"; shift 2;;
    *) echo "Unknown argument: $1" >&2; exit 2;;
  esac
done
[ -n "$TOKEN" ] && [ -n "$ENDPOINT" ] && [ -n "$MIGRATION_ID" ] && [ -n "$ROLLBACK_DEADLINE" ] || { echo "token, endpoint, migration-id and rollback-deadline are required" >&2; exit 2; }
command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 1; }
kubectl get deployment hypercdr-comm-agent -n "$NAMESPACE" >/dev/null
if kubectl get secret hypercdr-agent-handover -n "$NAMESPACE" >/dev/null 2>&1; then echo "An Agent handover is already active" >&2; exit 1; fi
API_BASE="${ENDPOINT/ws:\/\//http://}"; API_BASE="${API_BASE/wss:\/\//https://}"; API_BASE="${API_BASE%/ws/agent}"
curl -fsS -H 'Content-Type: application/json' -d "{\"token\":\"$TOKEN\"}" "$API_BASE/api/v1/disaster-handovers/validate" >/dev/null
PREVIOUS_ENDPOINT="$(kubectl get secret hypercdr-agent-bootstrap -n "$NAMESPACE" -o jsonpath='{.data.HCDR_PLATFORM_ENDPOINT}' | base64 -d)"
PREVIOUS_CLUSTER_ID="$(kubectl get secret hypercdr-agent-credential -n "$NAMESPACE" -o jsonpath='{.data.HCDR_CLUSTER_ID}' | base64 -d)"
PREVIOUS_CREDENTIAL="$(kubectl get secret hypercdr-agent-credential -n "$NAMESPACE" -o jsonpath='{.data.HCDR_AGENT_CREDENTIAL}' | base64 -d)"
STATE="$(printf '{\"MigrationID\":\"%s\",\"Status\":\"prepared\",\"PreviousEndpoint\":\"%s\",\"PreviousClusterID\":\"%s\",\"PreviousCredential\":\"%s\",\"TargetEndpoint\":\"%s\",\"TargetInstallToken\":\"%s\",\"RollbackDeadline\":\"%s\"}' "$MIGRATION_ID" "$PREVIOUS_ENDPOINT" "$PREVIOUS_CLUSTER_ID" "$PREVIOUS_CREDENTIAL" "$ENDPOINT" "$TOKEN" "$ROLLBACK_DEADLINE")"
kubectl create secret generic hypercdr-agent-handover -n "$NAMESPACE" --from-literal=state.json="$STATE" --dry-run=client -o yaml | kubectl apply -f -
kubectl patch secret hypercdr-agent-bootstrap -n "$NAMESPACE" --type merge -p "{\"stringData\":{\"HCDR_PLATFORM_ENDPOINT\":\"$ENDPOINT\",\"HCDR_INSTALL_TOKEN\":\"$TOKEN\"}}"
kubectl delete secret hypercdr-agent-credential -n "$NAMESPACE"
kubectl rollout restart deployment/hypercdr-comm-agent -n "$NAMESPACE"
echo "Disaster handover initiated. Enterprise must verify and Commit before $ROLLBACK_DEADLINE or the Agent will roll back automatically."
`))
}
