package wsclient

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/inventory"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type countingInventory struct{ count atomic.Int32 }

func (collector *countingInventory) Collect() (inventory.Snapshot, error) {
	hash := strconv.Itoa(int(collector.count.Add(1)))
	return inventory.Snapshot{Hash: hash, Report: protocol.InventoryReportPayload{InventoryHash: hash, CollectedAt: time.Now()}}, nil
}

type runtimeProbe struct {
	kube.AgentUpgrader
	kube.ComponentLogCollector
	kube.VeleroRuntimeManager
	images        atomic.Int32
	statuses      atomic.Int32
	invalidations atomic.Int32
	blocked       bool
}

func (probe *runtimeProbe) PodImageStatus(ctx context.Context, _, _, _ string) (string, string, string, error) {
	probe.images.Add(1)
	if probe.blocked {
		<-ctx.Done()
		return "", "", "", ctx.Err()
	}
	return "agent:v1", "image-id", "digest", nil
}

func (probe *runtimeProbe) VeleroRuntimeStatus(ctx context.Context, _ string) (kube.VeleroRuntimeStatus, error) {
	probe.statuses.Add(1)
	return kube.VeleroRuntimeStatus{ServerReady: true}, ctx.Err()
}

func (probe *runtimeProbe) InvalidateRuntimeObservations() { probe.invalidations.Add(1) }

func runCollectionLoop(t *testing.T, cfg config.Config, probe *runtimeProbe, doneWhen func(int, int) bool) *countingInventory {
	t.Helper()
	finished := make(chan struct{})
	var criteriaMet atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		defer close(finished)
		_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
		heartbeats, inventories := 0, 0
		for {
			var message map[string]any
			if connection.ReadJSON(&message) != nil {
				return
			}
			switch message["type"] {
			case protocol.MessageAgentHeartbeat:
				heartbeats++
			case protocol.MessageAgentInventoryReport:
				inventories++
			}
			if doneWhen(heartbeats, inventories) {
				criteriaMet.Store(true)
				return
			}
		}
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	collector := &countingInventory{}
	cfg.StateDir = t.TempDir()
	client := NewWithDependencies(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, collector)
	client.agentRuntime = nil
	if probe != nil {
		client.agentRuntime = probe
	}
	client.handover = nil
	client.conn = connection
	stopped := make(chan error, 1)
	go func() { stopped <- client.RunHeartbeat() }()
	select {
	case <-finished:
		if !criteriaMet.Load() {
			t.Fatal("connection closed before expected messages were observed")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("collection loop did not produce expected messages")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("collection loop did not stop")
	}
	return collector
}

func TestHeartbeatDoesNotTriggerInventoryCollection(t *testing.T) {
	collector := runCollectionLoop(t, config.Config{HeartbeatInterval: 10 * time.Millisecond, InventoryInterval: time.Hour}, nil,
		func(heartbeats, inventories int) bool { return heartbeats >= 5 && inventories >= 1 })
	if collector.count.Load() != 1 {
		t.Fatalf("heartbeats triggered %d inventory collections", collector.count.Load())
	}
}

func TestInventoryUsesItsOwnConfiguredInterval(t *testing.T) {
	collector := runCollectionLoop(t, config.Config{HeartbeatInterval: time.Hour, InventoryInterval: 10 * time.Millisecond}, nil,
		func(_, inventories int) bool { return inventories >= 3 })
	if collector.count.Load() < 3 {
		t.Fatal("inventory interval was ignored")
	}
}

func TestSlowRuntimeDoesNotBlockHeartbeats(t *testing.T) {
	probe := &runtimeProbe{blocked: true}
	runCollectionLoop(t, config.Config{HeartbeatInterval: 10 * time.Millisecond, InventoryInterval: time.Hour}, probe,
		func(heartbeats, inventories int) bool { return heartbeats >= 5 && inventories >= 1 })
}

func TestRuntimeImagesAreLowFrequencyButForcedRefreshBypassesCache(t *testing.T) {
	client := New(config.Config{StateDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	probe := &runtimeProbe{}
	client.agentRuntime = probe
	client.collectRuntime(context.Background(), false)
	client.collectRuntime(context.Background(), false)
	if probe.images.Load() != 1 || probe.statuses.Load() != 2 {
		t.Fatal("image and runtime status intervals were not separated")
	}
	client.collectRuntime(context.Background(), true)
	if probe.images.Load() != 2 || probe.invalidations.Load() != 1 {
		t.Fatal("forced refresh reused old runtime configuration")
	}
}

func TestSuccessfulConfigurationTasksRequestFreshRuntimeObservation(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err == nil {
			accepted <- connection
		}
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	remote := <-accepted
	defer remote.Close()
	client := New(config.Config{StateDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.conn = connection
	for _, taskType := range []string{"cache-settings", "cache-cleanup", "agent-upgrade", "velero-upgrade", "oadp-upgrade", "backup"} {
		if err := client.sendTaskCompleted(protocol.TaskDispatchPayload{TaskID: taskType, CommandID: taskType, Type: taskType}, nil, "completed"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-client.runtimeRefresh:
			if taskType == "backup" {
				t.Fatal("ordinary backup invalidated runtime configuration")
			}
		default:
			if taskType != "backup" {
				t.Fatalf("%s did not request immediate runtime refresh", taskType)
			}
		}
	}
}

func TestVeleroObservationsAreSlowerOnlyWhileIdle(t *testing.T) {
	client := &Client{}
	now := time.Now()
	if client.veleroObservationDue(now.Add(-59*time.Second), now) {
		t.Fatal("idle observation was too frequent")
	}
	if !client.veleroObservationDue(now.Add(-time.Minute), now) {
		t.Fatal("idle observation did not run after one minute")
	}
	client.observedActiveBackup.Store(true)
	if !client.veleroObservationDue(now.Add(-5*time.Second), now) {
		t.Fatal("observed running backup was slowed down")
	}
	client.observedActiveBackup.Store(false)
	client.taskRuns = map[string]*abortableTaskRun{"drill": {}}
	if !client.veleroObservationDue(now.Add(-5*time.Second), now) {
		t.Fatal("active task was slowed down")
	}
	if client.veleroObservationDue(now.Add(-4*time.Second), now) {
		t.Fatal("active polling interval was ignored")
	}
}
