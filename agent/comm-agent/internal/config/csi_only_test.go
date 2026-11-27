package config

import (
	"reflect"
	"testing"
)

func TestLegacyCoordinationEnvironmentIsIgnored(t *testing.T) {
	for _, name := range []string{"HCDR_CONSISTENCY_APPLICATION_PROFILE", "HCDR_CONSISTENCY_JOURNAL_NAMESPACE", "HCDR_CONSISTENCY_RECOVERY_HEARTBEAT"} {
		t.Setenv(name, "")
	}
	baseline := Load()
	t.Setenv("HCDR_CONSISTENCY_APPLICATION_PROFILE", "paired-ledger-v1")
	t.Setenv("HCDR_CONSISTENCY_JOURNAL_NAMESPACE", "hcdr-consistency-test")
	t.Setenv("HCDR_CONSISTENCY_RECOVERY_HEARTBEAT", "consistency-recovery-heartbeat")
	if !reflect.DeepEqual(baseline, Load()) {
		t.Fatal("legacy application coordination environment changed Agent configuration")
	}
}
