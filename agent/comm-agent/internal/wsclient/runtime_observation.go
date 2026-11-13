package wsclient

import (
	"context"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type runtimeObservation struct {
	agentImage   string
	agentImageID string
	agentDigest  string
	imageAt      time.Time
	statusAt     time.Time
	velero       kube.VeleroRuntimeStatus
}

func (c *Client) requestRuntimeRefresh() {
	select {
	case c.runtimeRefresh <- struct{}{}:
	default:
	}
}

func (c *Client) observeRuntime(ctx context.Context) {
	if c.agentRuntime == nil {
		return
	}
	interval := c.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.collectRuntime(ctx, false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectRuntime(ctx, false)
		case <-c.runtimeRefresh:
			c.collectRuntime(ctx, true)
		}
	}
}

func (c *Client) collectRuntime(ctx context.Context, force bool) {
	if ctx.Err() != nil {
		return
	}
	c.runtimeObservationMu.RLock()
	observed := c.runtimeObservation
	c.runtimeObservationMu.RUnlock()
	previous := observed
	if force {
		if invalidator, ok := c.agentRuntime.(interface{ InvalidateRuntimeObservations() }); ok {
			invalidator.InvalidateRuntimeObservations()
		}
	}
	if force || observed.imageAt.IsZero() || time.Since(observed.imageAt) >= 5*time.Minute {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		image, imageID, digest, err := c.agentRuntime.PodImageStatus(queryCtx, c.cfg.Namespace, c.cfg.PodName, "comm-agent")
		cancel()
		if err != nil {
			c.logger.Warn("failed to read agent pod image status", "error", err)
		} else {
			observed.agentImage, observed.agentImageID, observed.agentDigest = image, imageID, digest
			observed.imageAt = time.Now()
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	status, err := c.agentRuntime.VeleroRuntimeStatus(queryCtx, c.cfg.Namespace)
	cancel()
	if err != nil {
		c.logger.Warn("failed to read velero runtime status", "error", err)
		observed.velero.ServerReady = false
		observed.velero.NodeAgentReady = 0
		observed.velero.CacheState = nil
	} else {
		observed.velero = status
		observed.statusAt = time.Now()
	}
	if ctx.Err() != nil {
		return
	}
	c.runtimeObservationMu.Lock()
	c.runtimeObservation = observed
	c.runtimeObservationMu.Unlock()
	changed := observed.velero.ServerReady != previous.velero.ServerReady || observed.velero.NodeAgentReady != previous.velero.NodeAgentReady || observed.velero.NodeAgentDesired != previous.velero.NodeAgentDesired
	if c.conn != nil && (force || previous.statusAt.IsZero() || changed) {
		if err := c.sendHeartbeat(false); err != nil {
			c.logger.Warn("failed to report runtime observation", "error", err)
		}
	}
}

func (c *Client) activeTaskCount() int {
	c.taskRunMu.Lock()
	count := len(c.taskRuns)
	c.taskRunMu.Unlock()
	c.cacheSettingsRunning.Range(func(_, _ any) bool { count++; return true })
	c.cacheCleanupRunning.Range(func(_, _ any) bool { count++; return true })
	return count
}

func (c *Client) veleroObservationDue(previous, now time.Time) bool {
	interval := time.Minute
	if c.activeTaskCount() > 0 || c.observedActiveBackup.Load() {
		interval = 5 * time.Second
	}
	return previous.IsZero() || now.Sub(previous) >= interval
}
