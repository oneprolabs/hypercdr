package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestClusterListProfilesReflectCurrentAuthenticatedAgent(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "profiles")
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	connection := &websocket.Conn{}
	router.hub.set(cluster.ID, connection)
	router.hub.setApplicationConsistencySupport(cluster.ID, connection, &protocol.ApplicationConsistencyCapability{Version: protocol.ApplicationConsistencyCapabilityVersion, Profiles: []string{"qualified"}})
	for _, connected := range []bool{true, false} {
		if !connected {
			router.hub.remove(cluster.ID, connection)
		}
		req := httptest.NewRequest("GET", "/api/v1/clusters", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, actor))
		recorder := httptest.NewRecorder()
		router.listClusters(recorder, req)
		var response struct {
			Items []store.Cluster `json:"items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
			t.Fatalf("cluster listing failed: %v", err)
		}
		found := false
		for _, item := range response.Items {
			if item.ID == cluster.ID {
				found = true
				if connected && (len(item.ApplicationConsistencyProfiles) != 1 || item.ApplicationConsistencyProfiles[0] != "qualified") {
					t.Fatal("cluster API omitted current profile")
				}
				if !connected && len(item.ApplicationConsistencyProfiles) != 0 {
					t.Fatal("cluster API retained a disconnected profile")
				}
			}
		}
		if !found {
			t.Fatal("authorized cluster missing")
		}
	}
}
