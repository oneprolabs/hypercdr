package wsclient

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/cacheusage"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type nodeCacheUsageCollector interface {
	CollectNodeCacheUsage(context.Context, string, string) ([]cacheusage.Node, error)
}

type cacheUsageFlight struct {
	done   chan struct{}
	report map[string]any
}

type cacheUsageObservation struct {
	mu             sync.Mutex
	ctx            context.Context
	flight         *cacheUsageFlight
	previous       []cacheusage.Node
	nextCollection time.Time
}

func (c *Client) nodeCacheUsageSupported() bool {
	_, supported := c.agentRuntime.(nodeCacheUsageCollector)
	return supported
}

func (c *Client) observeCacheUsage(ctx context.Context) {
	if !c.nodeCacheUsageSupported() {
		return
	}
	for {
		if delay := c.cacheUsageDelay(time.Now()); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				continue
			}
		}
		report := c.collectCacheUsage(ctx)
		if ctx.Err() != nil {
			return
		}
		if err := c.sendCacheUsageReport(report, protocol.Message[protocol.InventoryRequestPayload]{}); err != nil {
			c.logger.Warn("failed to report node cache usage", "error", err)
		}
	}
}

func (c *Client) cacheUsageDelay(now time.Time) time.Duration {
	c.cacheUsage.mu.Lock()
	defer c.cacheUsage.mu.Unlock()
	if delay := c.cacheUsage.nextCollection.Sub(now); delay > 0 {
		return delay
	}
	return 0
}

func (c *Client) collectCacheUsage(ctx context.Context) map[string]any {
	c.cacheUsage.mu.Lock()
	if flight := c.cacheUsage.flight; flight != nil {
		c.cacheUsage.mu.Unlock()
		select {
		case <-flight.done:
			return flight.report
		case <-ctx.Done():
			return map[string]any{"phase": "failed", "error": ctx.Err().Error(), "observedAt": time.Now().UTC().Format(time.RFC3339Nano)}
		}
	}
	flight := &cacheUsageFlight{done: make(chan struct{})}
	c.cacheUsage.flight = flight
	c.cacheUsage.mu.Unlock()
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	nodes, err := c.agentRuntime.(nodeCacheUsageCollector).CollectNodeCacheUsage(queryCtx, c.cfg.Namespace, c.cfg.PodName)
	cancel()
	c.cacheUsage.mu.Lock()
	defer c.cacheUsage.mu.Unlock()
	phase, errorMessage := "succeeded", ""
	if err != nil {
		phase, errorMessage = "failed", err.Error()
		nodes = c.cacheUsage.previous
	} else {
		nodes = cacheusage.PreservePreviousUsage(c.cacheUsage.previous, nodes)
		c.cacheUsage.previous = nodes
	}
	raw, _ := json.Marshal(nodes)
	var nodeValues []any
	_ = json.Unmarshal(raw, &nodeValues)
	flight.report = map[string]any{"phase": phase, "error": errorMessage, "observedAt": time.Now().UTC().Format(time.RFC3339Nano), "nodes": nodeValues}
	c.cacheUsage.flight = nil
	c.cacheUsage.nextCollection = time.Now().Add(time.Hour)
	close(flight.done)
	return flight.report
}

func (c *Client) handleCacheUsageRequest(request protocol.Message[protocol.InventoryRequestPayload]) {
	if !c.nodeCacheUsageSupported() {
		_ = c.sendMessageError(request.MessageID, protocol.MessagePlatformInventoryRequest, request.Payload.RequestID, "", "", "CACHE_USAGE_UNSUPPORTED", "Upgrade the communication agent to collect node cache usage", false)
		return
	}
	c.cacheUsage.mu.Lock()
	ctx := c.cacheUsage.ctx
	c.cacheUsage.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	report := c.collectCacheUsage(ctx)
	if ctx.Err() == nil {
		if err := c.sendCacheUsageReport(report, request); err != nil {
			c.logger.Warn("failed to respond to node cache usage request", "error", err)
		}
	}
}

func (c *Client) sendCacheUsageReport(report map[string]any, request protocol.Message[protocol.InventoryRequestPayload]) error {
	payload := protocol.InventoryReportPayload{Scope: "cache-usage", CacheUsage: report, RequestID: request.Payload.RequestID, AckMessageID: request.MessageID, AckType: protocol.MessagePlatformInventoryRequest, CollectedAt: time.Now().UTC()}
	kind := protocol.MessageKindEvent
	if request.Payload.RequestID != "" {
		kind = protocol.MessageKindResponse
	}
	return c.writeJSON(protocol.NewMessage(kind, protocol.MessageAgentInventoryReport, c.cfg.ClusterID, c.cfg.AgentID, payload))
}
