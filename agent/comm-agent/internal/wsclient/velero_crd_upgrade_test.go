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

func TestApplyVeleroCRDsUsesConnectedPlatformForPublishedBundle(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%t", secure), func(t *testing.T) {
			requests := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				if req.URL.Path != "/assets/velero/v1.18.2/crds.yaml" {
					t.Errorf("unexpected path: %s", req.URL.Path)
				}
				_, _ = fmt.Fprint(w, "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: backups.velero.io\n")
			})
			server := httptest.NewUnstartedServer(handler)
			if secure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			applier := &recordingManifestApplier{}
			client := &Client{cfg: config.Config{PlatformEndpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent", PlatformTLSSkipVerify: secure}, applier: applier}
			if err := client.applyVeleroCRDs(context.Background(), "https://public-platform.invalid/assets/velero/v1.18.2/crds.yaml"); err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(applier.objects) != 1 {
				t.Fatalf("requests=%d objects=%d", requests, len(applier.objects))
			}
		})
	}
}

func TestApplyVeleroCRDsRejectsCrossOriginRedirect(t *testing.T) {
	foreignRequests := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		foreignRequests++
	}))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, foreign.URL+"/crds.yaml", http.StatusFound)
	}))
	defer server.Close()
	client := &Client{cfg: config.Config{PlatformEndpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"}, applier: &recordingManifestApplier{}}
	err := client.applyVeleroCRDs(context.Background(), server.URL+"/assets/velero/v1.18.2/crds.yaml")
	if err == nil || !strings.Contains(err.Error(), "connected platform origin") || foreignRequests != 0 {
		t.Fatalf("error=%v foreign requests=%d", err, foreignRequests)
	}
}
