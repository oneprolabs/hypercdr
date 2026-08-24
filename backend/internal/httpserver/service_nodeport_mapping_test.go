package httpserver

import "testing"

func TestCollectNodePortsFromInventory(t *testing.T) {
	result := map[int]string{}
	collectNodePorts(map[string]any{"categories": []any{map[string]any{"items": []any{map[string]any{"resources": []any{map[string]any{"name": "web", "fields": map[string]any{"PORT(S)": "80:30081/TCP,443:30443/TCP"}}}}}}}}, "demo", result)
	if result[30081] != "demo/web" || result[30443] != "demo/web" {
		t.Fatalf("ports=%#v", result)
	}
}
