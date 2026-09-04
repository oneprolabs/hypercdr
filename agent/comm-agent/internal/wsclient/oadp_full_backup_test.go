package wsclient

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestValidateOADPFullBackupAcceptsUnfilteredSelection(t *testing.T) {
	for _, mode := range []string{"", "all"} {
		if err := validateOADPFullBackup(protocol.BackupCommand{ResourceSelection: protocol.ResourceSelection{Mode: mode}}); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
}

func TestValidateOADPFullBackupRejectsFilters(t *testing.T) {
	commands := []protocol.BackupCommand{
		{ResourceSelection: protocol.ResourceSelection{Mode: "custom", NamespaceScoped: []string{"deployments.apps"}}},
		{IncludedResources: []string{"pods"}},
		{ExcludedResources: []string{"secrets"}},
		{LabelSelector: protocol.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}},
	}
	for index, command := range commands {
		if err := validateOADPFullBackup(command); err == nil {
			t.Fatalf("filtered command %d was accepted", index)
		}
	}
}

func TestValidateOADPFullRestore(t *testing.T) {
	if err := validateOADPFullRestore(protocol.RestoreCommand{RestoreMode: "full", ArtifactMode: "all"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []protocol.RestoreCommand{{ArtifactMode: "volumes"}, {RestoreMode: "partial"}, {ExcludedResources: []string{"secrets"}}} {
		if err := validateOADPFullRestore(command); err == nil {
			t.Fatalf("filtered restore was accepted: %#v", command)
		}
	}
}

func TestOADPStorageSupportsOnlyS3CompatibleRepositories(t *testing.T) {
	for _, storageType := range []string{"", "aws", "AWS", "s3", "S3-Compatible", "s3 compatible"} {
		if !isOADPS3StorageType(storageType) {
			t.Fatalf("expected %q to be supported", storageType)
		}
	}
	for _, storageType := range []string{"azure", "gcp", "swift"} {
		if isOADPS3StorageType(storageType) {
			t.Fatalf("expected %q to be rejected", storageType)
		}
	}
}

func TestValidateOADPFullSchedule(t *testing.T) {
	if err := validateOADPFullSchedule(protocol.ScheduleSyncCommand{ResourceSelection: protocol.ResourceSelection{Mode: "all"}}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []protocol.ScheduleSyncCommand{
		{ResourceSelection: protocol.ResourceSelection{Mode: "custom"}},
		{IncludedResources: []string{"pods"}},
		{LabelSelector: "app=demo"},
		{Selector: protocol.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}},
		{ExcludeResources: []protocol.ExcludeRule{{Group: "", Resource: "events"}}},
	} {
		if err := validateOADPFullSchedule(command); err == nil {
			t.Fatalf("filtered schedule was accepted: %#v", command)
		}
	}
}
