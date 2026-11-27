package httpserver

import (
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestApplicationConsistencySupportBelongsToCurrentConnection(t *testing.T) {
	hub := newSessionHub()
	oldConnection, currentConnection := &websocket.Conn{}, &websocket.Conn{}
	capability := &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"qualified"}}
	hub.set("cluster", oldConnection)
	hub.setApplicationConsistencySupport("cluster", oldConnection, capability)
	capability.Profiles[0] = "mutated"
	if !hub.supportsApplicationConsistency("cluster", "qualified") || hub.supportsApplicationConsistency("other", "qualified") || hub.supportsApplicationConsistency("cluster", "mutated") {
		t.Fatal("session support is not detached and cluster-scoped")
	}
	hub.set("cluster", currentConnection)
	if hub.supportsApplicationConsistency("cluster", "qualified") {
		t.Fatal("replacement connection inherited old capability")
	}
	capability.Profiles[0] = "qualified"
	hub.setApplicationConsistencySupport("cluster", oldConnection, capability)
	if hub.supportsApplicationConsistency("cluster", "qualified") {
		t.Fatal("stale connection restored capability")
	}
	hub.setApplicationConsistencySupport("cluster", currentConnection, capability)
	hub.remove("cluster", oldConnection)
	if !hub.supportsApplicationConsistency("cluster", "qualified") {
		t.Fatal("stale disconnect removed current capability")
	}
	hub.setApplicationConsistencySupport("cluster", currentConnection, &protocol.ApplicationConsistencyCapability{Version: "unknown", Profiles: []string{"qualified"}})
	if hub.supportsApplicationConsistency("cluster", "qualified") {
		t.Fatal("invalid advertisement retained support")
	}
	hub.setApplicationConsistencySupport("cluster", currentConnection, capability)
	hub.remove("cluster", currentConnection)
	if hub.supportsApplicationConsistency("cluster", "qualified") {
		t.Fatal("disconnected agent retained support")
	}
}

func TestApplicationConsistencyProfileInventoryUsesOnlyCurrentSocket(t *testing.T) {
	hub := newSessionHub()
	connection := &websocket.Conn{}
	hub.set("cluster", connection)
	hub.setApplicationConsistencySupport("cluster", connection, &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"z-profile", "a-profile"}})
	profiles := hub.applicationConsistencyProfiles("cluster")
	if len(profiles) != 2 || profiles[0] != "a-profile" || profiles[1] != "z-profile" {
		t.Fatalf("unexpected profile inventory: %v", profiles)
	}
	profiles[0] = "forged"
	if hub.applicationConsistencyProfiles("cluster")[0] != "a-profile" {
		t.Fatal("caller mutated authenticated profile inventory")
	}
	replacement := &websocket.Conn{}
	hub.set("cluster", replacement)
	if len(hub.applicationConsistencyProfiles("cluster")) != 0 {
		t.Fatal("replacement socket inherited old profiles")
	}
	hub.setApplicationConsistencySupport("cluster", connection, &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"stale"}})
	if len(hub.applicationConsistencyProfiles("cluster")) != 0 {
		t.Fatal("stale socket restored profiles")
	}
	hub.setApplicationConsistencySupport("cluster", replacement, &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"current"}})
	hub.remove("cluster", replacement)
	if len(hub.applicationConsistencyProfiles("cluster")) != 0 {
		t.Fatal("disconnected profile inventory survived")
	}
}
