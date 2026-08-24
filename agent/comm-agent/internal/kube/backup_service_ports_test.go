package kube

import (
	"encoding/json"
	"testing"
)

func TestBackupServicePortsReadsJSONNumbers(t *testing.T) {
	var object map[string]any
	if err := json.Unmarshal([]byte(`{
		"kind":"Service",
		"spec":{"ports":[{"name":"http","port":80,"protocol":"TCP","nodePort":30081}]}
	}`), &object); err != nil {
		t.Fatal(err)
	}
	ports := backupServicePorts(object)
	if len(ports) != 1 || ports[0].Name != "http" || ports[0].Port != 80 || ports[0].Protocol != "TCP" || ports[0].NodePort != 30081 {
		t.Fatalf("unexpected Service ports: %#v", ports)
	}
}
