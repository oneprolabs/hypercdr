package kube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestCollectPlanCapacityDeduplicatesRepositoryAndAllowsPartialFailure(t *testing.T) {
	var mutex sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		prefix := request.URL.Query().Get("prefix")
		mutex.Lock()
		counts[prefix]++
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/xml")
		if strings.Contains(prefix, "bad") {
			writer.WriteHeader(403)
			fmt.Fprint(writer, `<Error><Code>AccessDenied</Code><Message>Denied</Message></Error>`)
			return
		}
		size := 10
		if strings.Contains(prefix, "/kopia/") {
			size = 100
		}
		fmt.Fprintf(writer, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>test</Name><IsTruncated>false</IsTruncated><Contents><Key>%sobject</Key><Size>%d</Size></Contents></ListBucketResult>`, prefix, size)
	}))
	defer server.Close()
	bsl := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "BackupStorageLocation", "metadata": map[string]any{"name": "store", "namespace": "agent"}, "spec": map[string]any{"provider": "aws", "objectStorage": map[string]any{"bucket": "test", "prefix": "cluster"}, "credential": map[string]any{"name": "creds", "key": "cloud"}, "config": map[string]any{"s3Url": server.URL, "region": "us-east-1", "s3ForcePathStyle": "true"}}}}
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "creds", "namespace": "agent"}, "stringData": map[string]any{"cloud": "[default]\naws_access_key_id=test\naws_secret_access_key=test\n"}}}
	applier := NewDynamicManifestApplierWithClient(fake.NewSimpleDynamicClient(runtime.NewScheme(), bsl, secret))
	plans := []PlanCapacityTarget{{PlanID: "first", StorageLocation: "store", BackupNamePrefix: "first", Namespaces: []string{"shared"}}, {PlanID: "bad", StorageLocation: "store", BackupNamePrefix: "bad", Namespaces: []string{"shared"}}, {PlanID: "second", StorageLocation: "store", BackupNamePrefix: "second", Namespaces: []string{"shared"}}}
	results := applier.CollectPlanCapacity(context.Background(), "agent", plans)
	if len(results) != 3 || results[0].Error != nil || results[1].Error == nil || results[2].Error != nil {
		t.Fatalf("unexpected results %+v", results)
	}
	if results[0].Stats.TotalBytes != 110 || results[2].Stats.TotalBytes != 110 {
		t.Fatalf("incorrect capacity %+v", results)
	}
	if counts["cluster/kopia/shared/"] != 1 {
		t.Fatalf("repository scanned repeatedly: %v", counts)
	}
}
