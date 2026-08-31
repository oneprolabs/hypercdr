package wsclient

import (
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
)

func TestOrderedPlatformEndpointsPrefersPrimaryAndDeduplicates(t *testing.T) {
	cfg := config.Config{PlatformPrivateEndpoint: "wss://legacy-private/ws/agent", PlatformPublicEndpoint: "wss://public/ws/agent", PlatformEndpoint: "wss://primary/ws/agent"}
	want := []string{"wss://primary/ws/agent", "wss://public/ws/agent", "wss://legacy-private/ws/agent"}
	if got := orderedPlatformEndpoints(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoints = %#v, want %#v", got, want)
	}

	cfg.PlatformPrivateEndpoint = cfg.PlatformEndpoint
	want = []string{"wss://primary/ws/agent", "wss://public/ws/agent"}
	if got := orderedPlatformEndpoints(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoints = %#v, want %#v", got, want)
	}
}

func TestOrderedPlatformEndpointsSupportsPrivateOnly(t *testing.T) {
	want := []string{"wss://private/ws/agent"}
	if got := orderedPlatformEndpoints(config.Config{PlatformPrivateEndpoint: want[0]}); !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoints = %#v", got)
	}
}

func TestSelectedEndpointRemainsFirstForProcessLifetime(t *testing.T) {
	// Register stores the endpoint that connected in PlatformEndpoint. On a
	// reconnect, that selected endpoint remains first and the process does not
	// switch back to the address that failed during startup.
	cfg := config.Config{PlatformEndpoint: "wss://primary/ws/agent", PlatformPublicEndpoint: "wss://public/ws/agent", PlatformPrivateEndpoint: "wss://legacy/ws/agent"}
	pinPlatformEndpoint(&cfg, "wss://public/ws/agent")
	want := []string{"wss://public/ws/agent"}
	if got := orderedPlatformEndpoints(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoints = %#v, want fixed runtime endpoint %#v", got, want)
	}
}

func TestPinPlatformEndpointTrimsAndClearsOtherCandidates(t *testing.T) {
	cfg := config.Config{PlatformEndpoint: "wss://primary/ws/agent", PlatformPublicEndpoint: "wss://public/ws/agent", PlatformPrivateEndpoint: "wss://legacy/ws/agent"}
	pinPlatformEndpoint(&cfg, "  wss://public/ws/agent  ")
	if cfg.PlatformEndpoint != "wss://public/ws/agent" || cfg.PlatformPublicEndpoint != "" || cfg.PlatformPrivateEndpoint != "" {
		t.Fatalf("selected endpoint was not pinned: %+v", cfg)
	}
}
