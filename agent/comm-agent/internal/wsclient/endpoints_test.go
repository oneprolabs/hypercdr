package wsclient

import (
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
)

func TestOrderedPlatformEndpointsPrefersPrivateAndDeduplicates(t *testing.T) {
	cfg := config.Config{PlatformPrivateEndpoint: "wss://private/ws/agent", PlatformPublicEndpoint: "wss://public/ws/agent", PlatformEndpoint: "wss://public/ws/agent"}
	want := []string{"wss://private/ws/agent", "wss://public/ws/agent"}
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

func TestUsingPublicFallback(t *testing.T) {
	client := &Client{cfg: config.Config{PlatformPrivateEndpoint: "wss://private/ws/agent", PlatformEndpoint: "wss://public/ws/agent"}}
	if !client.usingPublicFallback() {
		t.Fatal("expected public connection to be recognized as fallback")
	}
	client.cfg.PlatformEndpoint = client.cfg.PlatformPrivateEndpoint
	if client.usingPublicFallback() {
		t.Fatal("private connection must not be recognized as fallback")
	}
}
