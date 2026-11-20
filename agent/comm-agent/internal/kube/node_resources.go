package kube

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

type nodeResourceSummary struct {
	Node struct {
		CPU struct {
			Time           string  `json:"time"`
			UsageNanoCores *uint64 `json:"usageNanoCores"`
		} `json:"cpu"`
		Memory struct {
			Time            string  `json:"time"`
			WorkingSetBytes *uint64 `json:"workingSetBytes"`
		} `json:"memory"`
		Fs struct {
			Time          string  `json:"time"`
			CapacityBytes *uint64 `json:"capacityBytes"`
			UsedBytes     *uint64 `json:"usedBytes"`
		} `json:"fs"`
	} `json:"node"`
}

func decodeNodeResources(data []byte) (map[string]string, error) {
	var summary nodeResourceSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, err
	}
	result := map[string]string{}
	if value := summary.Node.CPU.UsageNanoCores; value != nil {
		result["cpuCores"] = strconv.FormatFloat(float64(*value)/1e9, 'f', -1, 64)
		result["cpuObservedAt"] = summary.Node.CPU.Time
	}
	if value := summary.Node.Memory.WorkingSetBytes; value != nil {
		result["memoryBytes"] = strconv.FormatUint(*value, 10)
		result["memoryObservedAt"] = summary.Node.Memory.Time
	}
	if value := summary.Node.Fs.CapacityBytes; value != nil {
		result["storageCapacityBytes"] = strconv.FormatUint(*value, 10)
		result["storageObservedAt"] = summary.Node.Fs.Time
	}
	if value := summary.Node.Fs.UsedBytes; value != nil {
		result["storageUsedBytes"] = strconv.FormatUint(*value, 10)
	}
	return result, nil
}

func (r *KubernetesClusterReader) collectNodeResources(ctx context.Context, nodes []Node) {
	client := r.clientset.CoreV1().RESTClient()
	if client == nil {
		return
	}
	if concrete, ok := client.(*rest.RESTClient); ok && concrete == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	semaphore := make(chan struct{}, 4)
	var pending sync.WaitGroup
	for index := range nodes {
		pending.Add(1)
		go func() {
			defer pending.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			queryCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			defer stop()
			data, err := client.Get().Resource("nodes").Name(nodes[index].Name).SubResource("proxy").Suffix("stats/summary").DoRaw(queryCtx)
			if err != nil {
				nodes[index].ResourceUsage = map[string]string{"error": "Node resource statistics are unavailable"}
				return
			}
			usage, err := decodeNodeResources(data)
			if err != nil {
				nodes[index].ResourceUsage = map[string]string{"error": "Node resource statistics are invalid"}
				return
			}
			nodes[index].ResourceUsage = usage
		}()
	}
	pending.Wait()
}
