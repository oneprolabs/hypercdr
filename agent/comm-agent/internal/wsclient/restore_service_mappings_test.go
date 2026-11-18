package wsclient

import (
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestRestoreServiceNodePortMappings(t *testing.T) {
	for _, clone := range []bool{false, true} {
		restore := &protocol.RestoreCommand{SourceNamespace: "one", SourceNamespaces: []string{"one", "two"}, TargetNamespace: "one", ServiceNodePortMappings: map[string]int{"one/web|80|TCP": 30080, "two/web|80|TCP": 30081}}
		expected := map[string]map[string]int{"one": {"web|80|TCP": 30080}, "two": {"web|80|TCP": 30081}}
		if clone {
			restore.TargetNamespaces = map[string]string{"one": "one-drill", "two": "two-drill"}
			expected = map[string]map[string]int{"one-drill": {"web|80|TCP": 30080}, "two-drill": {"web|80|TCP": 30081}}
		}
		got, err := restoreServiceNodePortMappings(protocol.TaskDispatchPayload{Restore: restore})
		if err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatalf("clone=%v got=%v err=%v", clone, got, err)
		}
	}
}

func TestRestoreServiceNodePortMappingsLegacyAndInvalid(t *testing.T) {
	restore := &protocol.RestoreCommand{SourceNamespace: "one", TargetNamespace: "target", ServiceNodePortMappings: map[string]int{"web|80|TCP": 30080}}
	got, err := restoreServiceNodePortMappings(protocol.TaskDispatchPayload{Restore: restore})
	if err != nil || got["target"]["web|80|TCP"] != 30080 {
		t.Fatalf("legacy got=%v err=%v", got, err)
	}
	restore.ServiceNodePortMappings = map[string]int{"other/web|80|TCP": 30080}
	if _, err := restoreServiceNodePortMappings(protocol.TaskDispatchPayload{Restore: restore}); err == nil {
		t.Fatal("unselected namespace accepted")
	}
}
