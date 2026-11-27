package protocol

import (
	"fmt"
	"strings"
)

const ApplicationConsistencyCapabilityVersion = "coordinated-offline-v1"

type ApplicationConsistencyCapability struct {
	Version  string   `json:"version"`
	Profiles []string `json:"profiles"`
}

func (capability ApplicationConsistencyCapability) Validate() error {
	if capability.Version != ApplicationConsistencyCapabilityVersion || len(capability.Profiles) == 0 || len(capability.Profiles) > 64 {
		return fmt.Errorf("unsupported application consistency capability")
	}
	seen := map[string]bool{}
	for _, profile := range capability.Profiles {
		if profile == "" || strings.TrimSpace(profile) != profile || len(profile) > 128 || seen[profile] {
			return fmt.Errorf("invalid or duplicate application consistency profile")
		}
		seen[profile] = true
	}
	return nil
}

type ConsistencyGroup struct {
	ID                 string                `json:"id"`
	Strategy           string                `json:"strategy"`
	ApplicationProfile string                `json:"applicationProfile"`
	MaxPauseSeconds    int                   `json:"maxPauseSeconds"`
	Workloads          []ConsistencyWorkload `json:"workloads"`
	Volumes            []ConsistencyVolume   `json:"volumes"`
}

type ConsistencyWorkload struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

type ConsistencyVolume struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	UID           string `json:"uid"`
	SnapshotClass string `json:"snapshotClass"`
}

func (group ConsistencyGroup) Validate(namespaces []string) error {
	if group.ID == "" || group.Strategy != "offline-v1" || group.ApplicationProfile == "" || group.MaxPauseSeconds < 1 || group.MaxPauseSeconds > 120 || len(group.Workloads) == 0 || len(group.Volumes) == 0 {
		return fmt.Errorf("consistency group requires an explicit offline profile, workload and volume inventory, and pause limit of 1–120 seconds")
	}
	selected := map[string]bool{}
	for _, namespace := range namespaces {
		if namespace == "" || namespace == "*" {
			return fmt.Errorf("consistency group requires explicit source namespaces")
		}
		selected[namespace] = true
	}
	workloads, identities := map[string]bool{}, map[string]bool{}
	for _, workload := range group.Workloads {
		key := workload.Namespace + "/" + workload.Kind + "/" + workload.Name
		if !selected[workload.Namespace] || workload.Name == "" || workload.UID == "" || (workload.Kind != "Deployment" && workload.Kind != "StatefulSet") || workloads[key] || identities[workload.UID] {
			return fmt.Errorf("invalid, duplicate or out-of-scope consistency workload")
		}
		workloads[key], identities[workload.UID] = true, true
	}
	volumes := map[string]bool{}
	for _, volume := range group.Volumes {
		key := volume.Namespace + "/" + volume.Name
		if !selected[volume.Namespace] || volume.Name == "" || volume.UID == "" || volume.SnapshotClass == "" || volumes[key] || identities[volume.UID] {
			return fmt.Errorf("invalid, duplicate or out-of-scope consistency volume")
		}
		volumes[key], identities[volume.UID] = true, true
	}
	return nil
}
