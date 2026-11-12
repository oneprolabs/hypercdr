package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCacheTransitionWireRoundTrip(t *testing.T) {
	command := CacheSettingsCommand{Namespace: "hypercdr-agent", Transition: &CacheSettingsTransition{IsolationID: strings.Repeat("a", 64), Nodes: []string{"node-one", "node-two"}, Paths: []string{"/data/cache/old"}, Previous: CacheConfigurationSnapshot{IndexEnabled: true, IndexMaxGiB: 5, IndexPath: "/data/cache/index", DrillEnabled: true, DrillMaxGiB: 60, DrillPath: "/data/cache/old"}}}
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CacheSettingsCommand
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(command, decoded) {
		t.Fatalf("transition snapshot lost in transport: %v", err)
	}
	var legacy CacheSettingsCommand
	if err := json.Unmarshal([]byte(`{"namespace":"hypercdr-agent","indexEnabled":true}`), &legacy); err != nil || legacy.Transition != nil {
		t.Fatalf("legacy cache command compatibility failed: %v", err)
	}
}
