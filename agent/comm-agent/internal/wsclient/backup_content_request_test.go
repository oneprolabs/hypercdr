package wsclient

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"

	"github.com/gorilla/websocket"
)

type recordingBackupContentReader struct {
	namespace  string
	backupName string
	limit      int
}

func (r *recordingBackupContentReader) ReadVeleroBackupContents(_ context.Context, namespace, backupName string, limit int) ([]kube.BackupContentResource, bool, error) {
	r.namespace = namespace
	r.backupName = backupName
	r.limit = limit
	return []kube.BackupContentResource{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Namespace:  "workload",
		Name:       "example",
		Group:      "apps",
		Resource:   "deployments",
		Images:     []string{"example:v1"},
	}}, true, nil
}

func TestReadMessagesDispatchesBackupContentRequestAndReportsContents(t *testing.T) {
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, req, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		serverConn <- conn
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	peer := <-serverConn
	defer peer.Close()
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	reader := &recordingBackupContentReader{}
	client := &Client{
		cfg:           config.Config{ClusterID: "cluster-1", AgentID: "agent-1"},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		conn:          conn,
		contentReader: reader,
	}
	request := protocol.Message[protocol.BackupContentRequestPayload]{
		Version:     protocol.Version,
		MessageID:   "message-1",
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformBackupContentRequest,
		Payload: protocol.BackupContentRequestPayload{
			RequestID:        "request-1",
			VeleroNamespace:  "hypercdr-enterprise-agent",
			VeleroBackupName: "backup-1",
		},
	}
	readResult := make(chan error, 1)
	go func() { readResult <- client.readMessages() }()
	if err := peer.WriteJSON(request); err != nil {
		t.Fatalf("write backup contents request: %v", err)
	}

	var report protocol.Message[protocol.BackupContentReportPayload]
	if err := peer.ReadJSON(&report); err != nil {
		t.Fatalf("read backup contents report: %v", err)
	}
	if report.Type != protocol.MessageAgentBackupContentReport || report.MessageKind != protocol.MessageKindResponse {
		t.Fatalf("unexpected report envelope: type=%q kind=%q", report.Type, report.MessageKind)
	}
	if reader.namespace != "hypercdr-enterprise-agent" || reader.backupName != "backup-1" || reader.limit != 5000 {
		t.Fatalf("unexpected reader call: namespace=%q backup=%q limit=%d", reader.namespace, reader.backupName, reader.limit)
	}
	if report.Payload.RequestID != "request-1" || !report.Payload.Truncated || len(report.Payload.Resources) != 1 {
		t.Fatalf("unexpected report payload: %+v", report.Payload)
	}
	resource := report.Payload.Resources[0]
	if resource.Kind != "Deployment" || resource.Name != "example" || resource.Namespace != "workload" {
		t.Fatalf("unexpected resource: %+v", resource)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close websocket peer: %v", err)
	}
	select {
	case err := <-readResult:
		if err == nil {
			t.Fatal("readMessages returned nil after websocket close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readMessages did not stop after websocket close")
	}
}
