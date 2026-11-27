package store

import "time"

func snapshotNamespaceCapability(capability ClusterCapability) bool {
	return capability.Type == "CSISnapshotVolumePrerequisite" || capability.Type == "CSISnapshotNamespaceInventory" || capability.Type == "ConsistencyWorkload" || capability.Type == "ConsistencyNamespaceInventory"
}

func mergeSnapshotNamespaceCapabilities(existing, incoming []ClusterCapability, namespace string, collectedAt time.Time) []ClusterCapability {
	merged := make([]ClusterCapability, 0, len(existing)+len(incoming))
	for _, capability := range existing {
		if snapshotNamespaceCapability(capability) && capability.Fields["namespace"] != namespace {
			merged = append(merged, capability)
		}
	}
	for _, capability := range incoming {
		if snapshotNamespaceCapability(capability) {
			if capability.Fields["namespace"] != namespace {
				continue
			}
			fields := make(map[string]string, len(capability.Fields)+1)
			for key, value := range capability.Fields {
				fields[key] = value
			}
			fields["collectedAt"] = collectedAt.UTC().Format(time.RFC3339Nano)
			capability.Fields = fields
		}
		merged = append(merged, capability)
	}
	return merged
}
