package wsclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type recordingManifestApplier struct {
	objects []kube.AppliedObject
}

func (a *recordingManifestApplier) ApplyManifest(_ context.Context, manifest kube.Manifest) (kube.AppliedObject, error) {
	object, err := kube.ObjectFromManifest(manifest)
	if err == nil {
		a.objects = append(a.objects, object)
	}
	return object, err
}

func TestApplyVeleroCRDsAppliesOnlyCRDBundleFromPlatform(t *testing.T) {
	bundle := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: backups.velero.io
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: restores.velero.io
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, bundle)
	}))
	defer server.Close()

	applier := &recordingManifestApplier{}
	client := &Client{cfg: config.Config{PlatformEndpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"}, applier: applier}
	if err := client.applyVeleroCRDs(context.Background(), server.URL+"/assets/velero/v1.18.2/crds.yaml"); err != nil {
		t.Fatalf("applyVeleroCRDs() error = %v", err)
	}
	if len(applier.objects) != 2 || applier.objects[0].Name != "backups.velero.io" || applier.objects[1].Name != "restores.velero.io" {
		t.Fatalf("applied objects = %#v", applier.objects)
	}
}

func TestApplyVeleroCRDsRejectsForeignHostAndNonCRD(t *testing.T) {
	applier := &recordingManifestApplier{}
	client := &Client{cfg: config.Config{PlatformEndpoint: "wss://platform.example/ws/agent"}, applier: applier}
	if err := client.applyVeleroCRDs(context.Background(), "https://other.example/crds.yaml"); err == nil || !strings.Contains(err.Error(), "does not match platform host") {
		t.Fatalf("foreign host error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unsafe\n")
	}))
	defer server.Close()
	client.cfg.PlatformEndpoint = "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"
	if err := client.applyVeleroCRDs(context.Background(), server.URL+"/crds.yaml"); err == nil || !strings.Contains(err.Error(), "unexpected object") {
		t.Fatalf("non-CRD error = %v", err)
	}
	if len(applier.objects) != 0 {
		t.Fatalf("unexpected applied objects = %#v", applier.objects)
	}
}

func TestApplyVeleroCRDsRejectsOversizedBundle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", veleroCRDBundleMaxBytes+1))
	}))
	defer server.Close()
	client := &Client{
		cfg:     config.Config{PlatformEndpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"},
		applier: &recordingManifestApplier{},
	}
	if err := client.applyVeleroCRDs(context.Background(), server.URL+"/crds.yaml"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized bundle error = %v", err)
	}
}
