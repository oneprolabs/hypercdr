package httpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
)

func TestLocalManifestTargetWithoutDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release-manifest.json")
	digest := "sha256:" + strings.Repeat("a", 64)
	body := fmt.Sprintf(`{"version":"1.0.83.20260923","componentManifest":{"comm-agent":{"version":"1.0.83.20260923","image":"registry.example/agent:1","imageDigest":%q}}}`, digest)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Router{cfg: config.Config{ReleaseManifestPath: path}}
	target, err := r.componentTarget(context.Background(), "comm-agent")
	if err != nil {
		t.Fatal(err)
	}
	if target.Version != "1.0.83.20260923" || target.ImageDigest != digest {
		t.Fatalf("unexpected target: %+v", target)
	}
	if _, err := r.componentTarget(context.Background(), "velero"); err == nil {
		t.Fatal("missing component accepted")
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.componentTarget(context.Background(), "comm-agent"); err == nil {
		t.Fatal("invalid manifest accepted")
	}
}
