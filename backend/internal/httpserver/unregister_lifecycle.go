package httpserver

import "hypercdr-platform/platform/backend/internal/store"

// One active process may see repeated reconnects during object-store cleanup.
// Keep one local worker per persisted task; the database marker survives handoff.
func (r *Router) startUnregisterCleanup(task store.Task) {
	r.unregisterMu.Lock()
	if r.unregisterCleaning == nil {
		r.unregisterCleaning = map[string]struct{}{}
	}
	if _, exists := r.unregisterCleaning[task.ID]; exists {
		r.unregisterMu.Unlock()
		return
	}
	r.unregisterCleaning[task.ID] = struct{}{}
	r.unregisterMu.Unlock()
	release := func() { r.unregisterMu.Lock(); delete(r.unregisterCleaning, task.ID); r.unregisterMu.Unlock() }
	if !r.startWorker(func() {
		defer release()
		current, found, err := r.store.GetTask(task.ID)
		if err != nil || !found || isTerminalTaskStatus(current.Status) || boolPayload(current.Payload, "unregisterPreflightCompleted") {
			return
		}
		cleanupRelationships := boolPayload(current.Payload, "cleanupProtectionRelationships")
		// Older persisted tasks already record the user's backup deletion decision.
		cleanupRelationships = cleanupRelationships || boolPayload(current.Payload, "deleteBackupData")
		r.cleanupAndDispatchUnregister(current, stringSlicePayload(current.Payload, "storageRepositoryIds"), cleanupRelationships)
	}) {
		release()
	}
}
