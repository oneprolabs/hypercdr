package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestMapInventoryNodesRetainsOptionalResourceObservations(t *testing.T) {
	nodes := mapInventoryNodes([]protocol.NodeInventory{{Name: "observed", Capacity: map[string]string{"cpu": "8"}, ResourceUsage: map[string]string{"cpuCores": "0", "storageCapacityBytes": "100"}}, {Name: "legacy"}})
	if nodes[0].ResourceUsage["cpuCores"] != "0" || nodes[0].ResourceUsage["storageCapacityBytes"] != "100" || nodes[0].Capacity["cpu"] != "8" {
		t.Fatalf("observation lost: %#v", nodes[0])
	}
	if nodes[1].ResourceUsage != nil {
		t.Fatal("legacy node acquired fabricated statistics")
	}
}
