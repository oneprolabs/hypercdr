package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct{ responses map[string]string }

func (f fakeRunner) Run(_ context.Context, _, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	value, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected fixed kubectl invocation: %s", key)
	}
	return []byte(value), nil
}

func TestInspectClusterUsesFixedReadOnlyQueries(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test-001",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "ap-southeast-1",
		"get nodes -o name": "node/node-1\nnode/node-2\n",
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true\ncsi-nas|false\n",
	}}
	result, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal")
	if err != nil {
		t.Fatal(err)
	}
	if result.ClusterName != "cce-test-001" || result.ServerVersion != "v1.35.3" || result.NodeCount != 2 || result.DefaultStorageClass != "csi-disk" {
		t.Fatalf("unexpected inspection: %#v", result)
	}
}

func TestInspectClusterRejectsNonCCE(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.33.0"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                 "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:  "kind://node-1",
	}}
	if _, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "kind"); err == nil || !strings.Contains(err.Error(), "Huawei Cloud CCE") {
		t.Fatalf("expected CCE rejection, got %v", err)
	}
}
