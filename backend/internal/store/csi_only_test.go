package store

import (
	"errors"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestCSIOnlyProtectionRejectsLegacyCoordination(t *testing.T) {
	for _, mode := range []string{"", "live-filesystem", "snapshot-crash-consistent"} {
		if err := validateCSIOnlyProtection(DataProtectionSettings{Mode: mode}); err != nil {
			t.Fatalf("compatible mode %q rejected: %v", mode, err)
		}
	}
	if err := validateCSIOnlyProtection(DataProtectionSettings{Mode: "application-consistent"}); !errors.Is(err, ErrDataProtectionSettings) {
		t.Fatalf("legacy coordination must require explicit reconfiguration: %v", err)
	}
}

func TestCSIOnlyPlanValidationRejectsGroupsBeforeDatabaseAccess(t *testing.T) {
	for _, mode := range []string{"", "live-filesystem", "snapshot-crash-consistent", "application-consistent"} {
		plan := ProtectionPlan{DataProtection: DataProtectionSettings{Mode: mode, Group: &protocol.ConsistencyGroup{}}}
		if _, err := validatePlanDataProtection(nil, &plan); !errors.Is(err, ErrDataProtectionSettings) {
			t.Fatalf("mode %q with legacy group must be rejected: %v", mode, err)
		}
	}
}
