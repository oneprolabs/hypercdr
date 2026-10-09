package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestContentAndLogResponsesAreBoundToAuthenticatedAgentCluster(t *testing.T) {
	repo := newTestStore(t)
	h := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer h.Close(ctx)
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "request-owner", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	connect := func(tenantID, name string) (*websocket.Conn, store.Cluster) {
		t.Helper()
		token, err := repo.CreateAgentToken(tenantID, "", "request-test", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/ws/agent", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetReadDeadline(time.Now().Add(6 * time.Second))
		if err := conn.WriteJSON(protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, Type: protocol.MessageAgentRegister, AgentID: name, Payload: protocol.RegisterPayload{InstallToken: token.Token, Cluster: protocol.ClusterSummary{Name: name}, Agent: protocol.AgentSummary{Version: "test"}, Velero: protocol.VeleroSummary{Status: "ready"}}}); err != nil {
			t.Fatal(err)
		}
		var accepted protocol.Message[protocol.RegisterAcceptedPayload]
		if err := conn.ReadJSON(&accepted); err != nil {
			t.Fatal(err)
		}
		if accepted.Type != protocol.MessagePlatformRegisterAccepted {
			t.Fatal("registration rejected")
		}
		ready := time.NewTicker(5 * time.Millisecond)
		defer ready.Stop()
		for !h.router.hub.has(accepted.Payload.ClusterID) {
			select {
			case <-ready.C:
			case <-ctx.Done():
				t.Fatal("agent registration did not finish")
			}
		}
		return conn, store.Cluster{ID: accepted.Payload.ClusterID, TenantID: tenantID, Name: name}
	}
	attacker, _ := connect(store.DefaultTenantID, "attacker")
	owner, ownerCluster := connect(tenant.ID, "owner")
	pongs := make(chan string, 1)
	attacker.SetPongHandler(func(data string) error { pongs <- data; return nil })
	attackerReaderDone := make(chan struct{})
	go func() {
		defer close(attackerReaderDone)
		for {
			if _, _, err := attacker.ReadMessage(); err != nil {
				return
			}
		}
	}()
	defer func() { attacker.Close(); <-attackerReaderDone }()
	point := seedContractRestorePoint(t, repo, ownerCluster, "request-backup")
	if _, _, err := repo.UpdateRestorePointState(store.RestorePointStateInput{ID: point.ID, Status: "available", Metadata: map[string]any{"contentIndex": restorePointIndex{Status: "failed"}}}); err != nil {
		t.Fatal(err)
	}
	barrier := func(id string) {
		t.Helper()
		// A pong proves the peer read the preceding forged data frame. Heartbeats
		// have no protocol acknowledgement and cannot provide this barrier.
		if err := attacker.WriteControl(websocket.PingMessage, []byte(id), time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		select {
		case pong := <-pongs:
			if pong != id {
				t.Fatal("wrong barrier acknowledgement")
			}
		case <-ctx.Done():
			t.Fatal("agent did not consume the forged frame")
		}
	}
	actor := store.User{ID: "owner-user", TenantID: tenant.ID, Role: "operator", Status: "active"}
	contentDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := tenantRequest(httptest.NewRequest("GET", "/api/v1/restore-points/"+point.ID+"/contents", nil), actor)
		req.SetPathValue("id", point.ID)
		w := httptest.NewRecorder()
		h.router.getRestorePointContents(w, req)
		contentDone <- w
	}()
	var contentRequest protocol.Message[protocol.BackupContentRequestPayload]
	if err := owner.ReadJSON(&contentRequest); err != nil {
		t.Fatal(err)
	}
	if contentRequest.Type != protocol.MessagePlatformBackupContentRequest {
		t.Fatal("wrong content request")
	}
	fake := protocol.Message[protocol.BackupContentReportPayload]{Type: protocol.MessageAgentBackupContentReport, ClusterID: ownerCluster.ID, Payload: protocol.BackupContentReportPayload{RequestID: contentRequest.Payload.RequestID, ErrorCode: "FORGED", Message: "cross-cluster response"}}
	if err := attacker.WriteJSON(fake); err != nil {
		t.Fatal(err)
	}
	barrier("content-barrier")
	select {
	case <-contentDone:
		t.Fatal("foreign agent completed the content request")
	default:
	}
	fake.Payload.ErrorCode = ""
	fake.Payload.Message = ""
	if err := owner.WriteJSON(fake); err != nil {
		t.Fatal(err)
	}
	var w *httptest.ResponseRecorder
	select {
	case w = <-contentDone:
	case <-ctx.Done():
		t.Fatal("owner response not delivered")
	}
	if w.Code != 200 {
		t.Fatalf("content response %d %s", w.Code, w.Body.String())
	}
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
	applyRestorePointPayloadContract("GET /api/v1/restore-points/{id}/contents", op)
	validateWireObject(t, value, op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any))
	if value["source"] != "indexed_now" || value["resources"] != nil {
		t.Fatal("legacy empty report shape changed")
	}
	// Once saved, the catalog opens without an agent and has a normalized array.
	h.router.hub.close(ownerCluster.ID)
	cached := restorePointContractRun(t, h.router, actor, "GET /api/v1/restore-points/{id}/contents", "/api/v1/restore-points/"+point.ID+"/contents", "", point.ID, h.router.getRestorePointContents, 200)
	if cached["source"] != "index" || len(cached["resources"].([]any)) != 0 {
		t.Fatal("persisted empty catalog not normalized")
	}
	// Use a separate online agent for log requests; leave the cached catalog offline.
	logOwner, logCluster := connect(tenant.ID, "log-owner")
	type logResult struct {
		status int
		err    error
	}
	logDone := make(chan logResult, 1)
	go func() {
		_, _, status, err := h.router.requestClusterLogs(logCluster.ID, "comm-agent", time.Now().Add(-time.Minute), 10)
		logDone <- logResult{status, err}
	}()
	var logRequest protocol.Message[protocol.LogRequestPayload]
	if err := logOwner.ReadJSON(&logRequest); err != nil {
		t.Fatal(err)
	}
	forgedLog := protocol.Message[protocol.LogReportPayload]{Type: protocol.MessageAgentLogReport, ClusterID: logCluster.ID, Payload: protocol.LogReportPayload{RequestID: logRequest.Payload.RequestID, ErrorCode: "FORGED", Message: "cross-cluster log response"}}
	if err := attacker.WriteJSON(forgedLog); err != nil {
		t.Fatal(err)
	}
	barrier("log-barrier")
	select {
	case <-logDone:
		t.Fatal("foreign agent completed the log request")
	default:
	}
	forgedLog.Payload.ErrorCode = ""
	forgedLog.Payload.Message = ""
	if err := logOwner.WriteJSON(forgedLog); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-logDone:
		if result.err != nil || result.status != 200 {
			t.Fatalf("owner log response: %#v", result)
		}
	case <-ctx.Done():
		t.Fatal("owner log response not delivered")
	}
	h.router.backupContentRequestMu.Lock()
	contentWaiters := len(h.router.backupContentRequests)
	h.router.backupContentRequestMu.Unlock()
	h.router.logRequestMu.Lock()
	logWaiters := len(h.router.logRequests)
	h.router.logRequestMu.Unlock()
	if contentWaiters != 0 || logWaiters != 0 {
		t.Fatal("request waiters leaked")
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
