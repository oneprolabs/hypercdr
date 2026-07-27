package httpserver

import (
	"log/slog"
	"time"
)

const (
	diagnosticLogRetention       = 180 * 24 * time.Hour
	diagnosticLogCleanupInterval = 24 * time.Hour
	clusterLogArchiveInterval    = 24 * time.Hour
	clusterLogRetryInterval      = 15 * time.Minute
	clusterLogOverlap            = 5 * time.Minute
	clusterLogInitialLookback    = 24 * time.Hour
	// Keep the request compatible with already-installed agents. Components
	// that reach this limit are permanently moved to the shorter archive cycle.
	clusterLogTailLines = int64(2000)
)

var clusterLogComponents = []string{"comm-agent", "velero", "node-agent"}

func (r *Router) scheduleLogMaintenance(now time.Time) {
	r.logMaintMu.Lock()
	if r.logMaintRun {
		r.logMaintMu.Unlock()
		return
	}
	r.logMaintRun = true
	r.logMaintMu.Unlock()
	go func() {
		defer func() {
			r.logMaintMu.Lock()
			r.logMaintRun = false
			r.logMaintMu.Unlock()
		}()
		r.runDiagnosticLogCleanup(now)
		r.runClusterLogArchive(now)
	}()
}

func (r *Router) runDiagnosticLogCleanup(now time.Time) {
	r.logMaintMu.Lock()
	lastRun := r.logCleanupAt
	r.logMaintMu.Unlock()
	if !lastRun.IsZero() && now.Sub(lastRun) < diagnosticLogCleanupInterval {
		return
	}
	cutoff := now.Add(-diagnosticLogRetention)
	var total int64
	for batches := 0; batches < 100; batches++ {
		removed, err := r.store.PurgeDiagnosticLogs(cutoff)
		if err != nil {
			r.logger.Error("diagnostic log retention cleanup failed", "error", err)
			return
		}
		total += removed
		if removed < 10000 {
			break
		}
	}
	r.logMaintMu.Lock()
	r.logCleanupAt = now
	r.logMaintMu.Unlock()
	r.logger.Info("diagnostic log retention cleanup completed", "retention_days", 180, "removed", total)
}

func (r *Router) runClusterLogArchive(now time.Time) {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to list clusters for automatic log archive", "error", err)
		return
	}
	for _, cluster := range clusters {
		if !r.hub.has(cluster.ID) {
			continue
		}
		for _, component := range clusterLogComponents {
			key := cluster.ID + "::" + component
			r.logMaintMu.Lock()
			retryAfter := r.logRetryAfter[key]
			r.logMaintMu.Unlock()
			if retryAfter.After(now) {
				continue
			}
			coverage, found, err := r.store.GetClusterLogCoverage(cluster.ID, component)
			if err != nil {
				r.logger.Error("failed to read cluster log archive watermark", "cluster_id", cluster.ID, "component", component, "error", err)
				continue
			}
			interval := clusterLogArchiveInterval
			if found && coverage.Truncated {
				interval = clusterLogRetryInterval
			}
			if found && now.Sub(coverage.LastCollectedAt) < interval {
				continue
			}
			since := now.Add(-clusterLogInitialLookback)
			if found && coverage.CoveredTo.After(since) {
				since = coverage.CoveredTo.Add(-clusterLogOverlap)
			}
			report, saved, requestID, _, collectErr := r.collectClusterLogsRange(cluster.ID, component, since, clusterLogTailLines)
			if collectErr != nil {
				r.logMaintMu.Lock()
				r.logRetryAfter[key] = now.Add(clusterLogRetryInterval)
				r.logMaintMu.Unlock()
				r.logger.Warn("automatic cluster log archive failed", slog.String("cluster_id", cluster.ID), slog.String("component", component), slog.String("request_id", requestID), "error", collectErr)
				continue
			}
			r.logMaintMu.Lock()
			delete(r.logRetryAfter, key)
			r.logMaintMu.Unlock()
			r.logger.Info("automatic cluster log archive completed", slog.String("cluster_id", cluster.ID), slog.String("component", component), "entries", len(report.Entries), "truncated", report.Truncated, "covered_from", saved.CoveredFrom, "covered_to", saved.CoveredTo)
		}
	}
}
