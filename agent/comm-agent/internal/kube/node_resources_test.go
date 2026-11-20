package kube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestNodeResourcesUseRootFilesystemWithoutNodeAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/nodes/worker/proxy/stats/summary" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node":{"cpu":{"usageNanoCores":0},"memory":{"workingSetBytes":1024},"fs":{"capacityBytes":2048,"usedBytes":512}}}`))
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	reader := NewKubernetesClusterReaderWithClients(client, nil, nil)
	nodes := []Node{{Name: "worker"}, {Name: "unavailable"}}
	reader.collectNodeResources(context.Background(), nodes)
	if nodes[0].ResourceUsage["cpuCores"] != "0" || nodes[0].ResourceUsage["storageCapacityBytes"] != "2048" {
		t.Fatalf("missing node statistics: %#v", nodes[0])
	}
	if nodes[1].ResourceUsage["error"] == "" {
		t.Fatal("unavailable node is not marked unknown")
	}
	if _, exists := nodes[1].ResourceUsage["cpuCores"]; exists {
		t.Fatal("failed statistics became zero")
	}
}

func TestDecodeNodeResources(t *testing.T) {
	usage, err := decodeNodeResources([]byte(`{"node":{"cpu":{"time":"2026-11-20T01:00:00Z","usageNanoCores":1200000000},"memory":{"workingSetBytes":6000000000},"fs":{"capacityBytes":200000000000,"usedBytes":42000000000}}}`))
	if err != nil || usage["cpuCores"] != "1.2" || usage["memoryBytes"] != "6000000000" || usage["storageUsedBytes"] != "42000000000" || usage["storageCapacityBytes"] != "200000000000" || usage["cpuObservedAt"] != "2026-11-20T01:00:00Z" {
		t.Fatalf("unexpected observation: %#v, %v", usage, err)
	}
}

func TestDecodeNodeResourcesDistinguishesMissingAndZero(t *testing.T) {
	usage, err := decodeNodeResources([]byte(`{"node":{"cpu":{"usageNanoCores":0},"memory":{},"fs":{}}}`))
	if err != nil || usage["cpuCores"] != "0" {
		t.Fatalf("zero CPU lost: %#v, %v", usage, err)
	}
	if _, exists := usage["memoryBytes"]; exists {
		t.Fatal("missing memory became a zero measurement")
	}
	if _, exists := usage["storageUsedBytes"]; exists {
		t.Fatal("missing storage became a zero measurement")
	}
	if _, err := decodeNodeResources([]byte(`invalid`)); err == nil {
		t.Fatal("invalid statistics accepted")
	}
}
