package httpserver

import (
	"errors"
	"net/http"

	"hypercdr-platform/platform/backend/internal/store"
)

func writeWorkerEntitlementError(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, store.ErrWorkerNodeCapacity) {
		return false
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"error":   "worker_node_quota_exceeded",
		"message": "The worker-node quota has been exceeded and the 30-day grace period has ended. Increase the quota or change plan to start new Sync or Drill tasks. Other operations remain available.",
	})
	return true
}
