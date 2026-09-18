package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestRegistrationUsesConfiguredPort(t *testing.T) {
	for _, port := range []string{"12443", "18443"} {
		for _, clusterType := range []string{"native-kubernetes", "huaweicloud-cce", "openshift"} {
			t.Run(port+"/"+clusterType, func(t *testing.T) {
				base := "https://platform.example:" + port
				repo := store.NewMemoryStore()
				components := map[string]store.ReleaseComponent{}
				for _, name := range []string{"comm-agent", "velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp"} {
					components[name] = store.ReleaseComponent{Version: "test", Image: "registry.example/hypercdr/" + name + ":test", ImageDigest: "sha256:test"}
				}
				if _, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: "test", APIImage: "registry.example/api:test", APIImageDigest: "sha256:api", FrontendImage: "registry.example/frontend:test", FrontendImageDigest: "sha256:frontend", Status: "active", ComponentManifest: components}); err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(NewRouter(config.Config{BaseURL: base}, slog.New(slog.NewTextHandler(io.Discard, nil)), repo))
				defer server.Close()
				resp, err := http.Post(server.URL+"/api/v1/agent-tokens", "application/json", bytes.NewBufferString(`{"clusterType":"`+clusterType+`"}`))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				var result struct {
					InstallCommand string `json:"installCommand"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{base + "/install.sh", "wss://platform.example:" + port + "/ws/agent", "--cluster-type " + clusterType} {
					if !strings.Contains(result.InstallCommand, want) {
						t.Fatalf("command missing %q: %s", want, result.InstallCommand)
					}
				}
				resp, err = http.Get(server.URL + "/install.sh")
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{"/api/v1/agent-tokens/validate", "/uninstall-agent.sh", "/assets/registry/ca.crt", "/assets/platform/ca.crt", veleroCRDsPath} {
					if !strings.Contains(string(body), base+path) {
						t.Fatalf("script missing %s", base+path)
					}
				}
				if strings.Contains(string(body), ":3002") {
					t.Fatal("script contains stale port")
				}
			})
		}
	}
}

func TestPublicBaseURLPriorityAndForwardedPort(t *testing.T) {
	for _, tc := range []struct{ name, base, public, host, want string }{
		{"public", "https://private:18443", "https://public:22443", "proxy", "https://public:22443"},
		{"base", "https://private:18443", "", "proxy", "https://private:18443"},
		{"forwarded", "", "", "platform:24443", "https://platform:24443"},
		{"standard HTTPS", "", "", "platform", "https://platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Router{cfg: config.Config{BaseURL: tc.base, PublicBaseURL: tc.public}}
			req := httptest.NewRequest("GET", "http://internal/install.sh", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Host", tc.host)
			if got := r.publicBaseURL(req); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
