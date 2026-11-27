package protocol

import (
	"encoding/json"
	"testing"
)

func TestConsistencyGroupValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-id", "unknown-strategy", "missing-profile", "zero-pause", "long-pause", "missing-workload", "missing-volume", "unknown-kind", "outside-namespace", "duplicate-workload", "duplicate-volume", "missing-uid", "missing-class", "wildcard"} {
		t.Run(scenario, func(t *testing.T) {
			group := ConsistencyGroup{ID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified-profile-v1", MaxPauseSeconds: 120,
				Workloads: []ConsistencyWorkload{{Namespace: "app", Kind: "Deployment", Name: "writer", UID: "workload-uid"}},
				Volumes:   []ConsistencyVolume{{Namespace: "app", Name: "data", UID: "volume-uid", SnapshotClass: "snapshot-class"}}}
			namespaces := []string{"app"}
			switch scenario {
			case "missing-id":
				group.ID = ""
			case "unknown-strategy":
				group.Strategy = "live"
			case "missing-profile":
				group.ApplicationProfile = ""
			case "zero-pause":
				group.MaxPauseSeconds = 0
			case "long-pause":
				group.MaxPauseSeconds = 121
			case "missing-workload":
				group.Workloads = nil
			case "missing-volume":
				group.Volumes = nil
			case "unknown-kind":
				group.Workloads[0].Kind = "Pod"
			case "outside-namespace":
				group.Volumes[0].Namespace = "other"
			case "duplicate-workload":
				group.Workloads = append(group.Workloads, group.Workloads[0])
			case "duplicate-volume":
				group.Volumes = append(group.Volumes, group.Volumes[0])
			case "missing-uid":
				group.Volumes[0].UID = ""
			case "missing-class":
				group.Volumes[0].SnapshotClass = ""
			case "wildcard":
				namespaces = append(namespaces, "*")
			}
			if err := group.Validate(namespaces); (err == nil) != (scenario == "valid") {
				t.Fatalf("validation error=%v", err)
			}
		})
	}
}

func TestConsistencyGroupCommandWireCompatibility(t *testing.T) {
	legacy, err := json.Marshal(BackupCommand{SourceNamespace: "app"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(legacy, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["consistencyGroup"]; exists {
		t.Fatal("legacy command unexpectedly acquired consistency requirements")
	}
	for _, payload := range []any{BackupCommand{ConsistencyGroup: &ConsistencyGroup{ID: "group"}}, ScheduleSyncCommand{ConsistencyGroup: &ConsistencyGroup{ID: "group"}}} {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Group *ConsistencyGroup `json:"consistencyGroup"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil || decoded.Group == nil || decoded.Group.ID != "group" {
			t.Fatalf("consistency group lost in wire encoding: %s, %v", data, err)
		}
	}
}
func TestApplicationConsistencyCapabilityValidation(t *testing.T) {
	for _, scenario := range []struct {
		capability ApplicationConsistencyCapability
		valid      bool
	}{
		{ApplicationConsistencyCapability{Version: ApplicationConsistencyCapabilityVersion, Profiles: []string{"qualified"}}, true},
		{ApplicationConsistencyCapability{Version: "unknown", Profiles: []string{"qualified"}}, false},
		{ApplicationConsistencyCapability{Version: ApplicationConsistencyCapabilityVersion}, false},
		{ApplicationConsistencyCapability{Version: ApplicationConsistencyCapabilityVersion, Profiles: []string{"qualified", "qualified"}}, false},
		{ApplicationConsistencyCapability{Version: ApplicationConsistencyCapabilityVersion, Profiles: []string{" "}}, false},
	} {
		if (scenario.capability.Validate() == nil) != scenario.valid {
			t.Fatalf("capability validity mismatch: %#v", scenario.capability)
		}
	}
}
