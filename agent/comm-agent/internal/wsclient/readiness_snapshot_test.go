package wsclient

import (
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"testing"
	"time"
)

func TestReadinessSnapshotKeyAggregatesRepeatedWarnings(t *testing.T) {
	readiness := kube.NamespaceReadiness{Objects: []kube.ReadinessObject{{UID: "pod-1", State: "Starting", EventReason: "FailedMount", EventMessage: "waiting for attachment", EventCount: 1, EventAt: time.Now()}}}
	previous := readinessSnapshotKey(readiness)
	readiness.Objects[0].EventAt = time.Now().Add(time.Minute)
	readiness.Objects[0].EventCount = 5
	if readinessSnapshotKey(readiness) != previous {
		t.Fatal("repeated warning must not create a transition")
	}
	if readiness.Objects[0].EventCount != 5 {
		t.Fatal("signature must not mutate the snapshot")
	}
	readiness.Objects[0].State = "Ready"
	if readinessSnapshotKey(readiness) == previous {
		t.Fatal("state transition must create an event")
	}
}

func TestReadinessSnapshotKeyAggregatesImageEvents(t *testing.T) {
	readiness := kube.NamespaceReadiness{ImageEvents: []kube.ReadinessImageEvent{{ID: "event-1", Reason: "Failed", LastAt: time.Now(), Count: 1}}}
	previous := readinessSnapshotKey(readiness)
	readiness.ImageEvents[0].Count = 7
	readiness.ImageEvents[0].LastAt = time.Now().Add(time.Minute)
	if previous != readinessSnapshotKey(readiness) {
		t.Fatal("repeated image warnings must not create more transitions")
	}
	readiness.ImageEvents = append(readiness.ImageEvents, kube.ReadinessImageEvent{ID: "event-2", Reason: "Pulled"})
	if previous == readinessSnapshotKey(readiness) {
		t.Fatal("pull completion must create a transition")
	}
}
