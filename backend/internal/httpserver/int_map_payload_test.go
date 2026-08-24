package httpserver

import "testing"

func TestIntMapPayloadAcceptsNewTaskTypedMap(t *testing.T) {
	got := intMapPayload(map[string]any{
		"serviceNodePortMappings": map[string]int{"web|80|TCP": 30082},
	}, "serviceNodePortMappings")
	if got["web|80|TCP"] != 30082 {
		t.Fatalf("typed mapping was lost before first dispatch: %#v", got)
	}
}

func TestIntMapPayloadAcceptsJSONDecodedMap(t *testing.T) {
	got := intMapPayload(map[string]any{
		"serviceNodePortMappings": map[string]any{"web|80|TCP": float64(30082)},
	}, "serviceNodePortMappings")
	if got["web|80|TCP"] != 30082 {
		t.Fatalf("decoded mapping was lost: %#v", got)
	}
}
