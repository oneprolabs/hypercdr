package wsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/cacheusage"
)

type cacheUsageProbe struct {
	runtimeProbe
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	err     error
}

func (probe *cacheUsageProbe) CollectNodeCacheUsage(ctx context.Context, _, _ string) ([]cacheusage.Node, error) {
	probe.calls.Add(1)
	if probe.started != nil {
		select {
		case probe.started <- struct{}{}:
		default:
		}
	}
	if probe.release != nil {
		select {
		case <-probe.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	used := int64(123)
	return []cacheusage.Node{{Name: "worker", Index: cacheusage.Cache{Path: "/cache/index", UsedBytes: &used, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}, probe.err
}

func TestCacheUsageConcurrentCollectionIsSingleFlight(t *testing.T) {
	probe := &cacheUsageProbe{started: make(chan struct{}, 1), release: make(chan struct{})}
	client := &Client{agentRuntime: probe}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); client.collectCacheUsage(context.Background()) }()
	<-probe.started
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result := client.collectCacheUsage(cancelled)
	if result["phase"] != "failed" {
		t.Fatal("cancelled waiter did not exit")
	}
	if probe.calls.Load() != 1 {
		t.Fatal("concurrent waiter started another scan")
	}
	close(probe.release)
	wait.Wait()
}

func TestCacheUsageFailureRetainsLastMeasurement(t *testing.T) {
	probe := &cacheUsageProbe{}
	client := &Client{agentRuntime: probe}
	first := client.collectCacheUsage(context.Background())
	probe.err = errors.New("node offline")
	second := client.collectCacheUsage(context.Background())
	if second["phase"] != "failed" {
		t.Fatal("collection failure reported success")
	}
	firstNodes := first["nodes"].([]any)
	secondNodes := second["nodes"].([]any)
	firstCache := firstNodes[0].(map[string]any)["index"].(map[string]any)
	secondCache := secondNodes[0].(map[string]any)["index"].(map[string]any)
	if secondCache["usedBytes"] != firstCache["usedBytes"] || secondCache["observedAt"] != firstCache["observedAt"] {
		t.Fatal("failure erased previous measurement")
	}
}

func TestCacheUsageHourlyScheduleSurvivesReconnectAndManualRefresh(t *testing.T) {
	probe := &cacheUsageProbe{}
	client := &Client{agentRuntime: probe}
	if client.cacheUsageDelay(time.Now()) != 0 {
		t.Fatal("initial collection was delayed")
	}
	client.collectCacheUsage(context.Background())
	delay := client.cacheUsageDelay(time.Now())
	if delay < 59*time.Minute || delay > time.Hour {
		t.Fatalf("unexpected hourly delay: %s", delay)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.observeCacheUsage(ctx)
	if probe.calls.Load() != 1 {
		t.Fatal("reconnecting worker repeated a recent scan")
	}
	client.cacheUsage.mu.Lock()
	client.cacheUsage.nextCollection = time.Now().Add(-time.Second)
	client.cacheUsage.mu.Unlock()
	if client.cacheUsageDelay(time.Now()) != 0 {
		t.Fatal("hourly collection did not become due")
	}
	client.collectCacheUsage(context.Background())
	if client.cacheUsageDelay(time.Now()) < 59*time.Minute {
		t.Fatal("manual refresh did not postpone redundant periodic scan")
	}
}
