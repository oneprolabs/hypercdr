package httpserver

import (
	"bytes"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlueGreenReleaseWithoutUpgrader(t *testing.T) {
	for _, missing := range []string{"", "comm-agent", "velero"} {
		t.Run("missing="+missing, func(t *testing.T) {
			manifest := map[string]store.ReleaseComponent{}
			cache := map[string]imageDigestCacheEntry{}
			names := []string{"platform-api", "platform-frontend", "cluster-registration-executor", "comm-agent", "velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp", "oadp-comm-agent", "oadp-operator", "oadp-velero", "oadp-openshift-plugin", "oadp-aws-plugin", "oadp-restore-helper", "oadp-bundle", "oadp-catalog"}
			for _, name := range names {
				if name == missing {
					continue
				}
				image := "registry.example/hypercdr/" + name + ":test"
				digest := "sha256:" + strings.Repeat("a", 64)
				manifest[name] = store.ReleaseComponent{Version: "test", Image: image, ImageDigest: digest}
				cache[image] = imageDigestCacheEntry{Digest: digest, ExpiresAt: time.Now().Add(time.Hour)}
			}
			repo := store.NewMemoryStore()
			r := &Router{store: repo, imageDigests: cache}
			body, _ := json.Marshal(map[string]any{"version": "test", "componentManifest": manifest})
			w := httptest.NewRecorder()
			r.createPlatformRelease(w, httptest.NewRequest("POST", "/api/v1/platform/releases", bytes.NewReader(body)))
			if missing != "" {
				if w.Code != 400 || !strings.Contains(w.Body.String(), missing) {
					t.Fatalf("expected missing %s rejection: %d %s", missing, w.Code, w.Body.String())
				}
				return
			}
			if w.Code != 201 {
				t.Fatalf("register: %d %s", w.Code, w.Body.String())
			}
			releases, _ := repo.ListPlatformReleases()
			if len(releases) != 1 || releases[0].Status != "active" {
				t.Fatalf("expected active release: %#v", releases)
			}
		})
	}
}
