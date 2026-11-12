package wsclient

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/minio/minio-go/v7"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type forceCleanupApplier struct {
	kube.VeleroProtectionCleaner
	preflightError error
	localError     error
	deleted        []kube.AppliedObject
	remoteCalls    int
}

func (applier *forceCleanupApplier) ApplyManifest(context.Context, kube.Manifest) (kube.AppliedObject, error) {
	panic("force cleanup must not wait for a Velero deletion request")
}

func (applier *forceCleanupApplier) CheckProtectionCleanupStorage(context.Context, string, string) error {
	return applier.preflightError
}

func (applier *forceCleanupApplier) WaitForVeleroBackupRecordDeleted(context.Context, string, string) error {
	return applier.localError
}

func (applier *forceCleanupApplier) DeleteObject(_ context.Context, object kube.AppliedObject) error {
	applier.deleted = append(applier.deleted, object)
	return applier.localError
}

func (applier *forceCleanupApplier) DeleteVeleroBackupArtifactsByNamePrefix(context.Context, string, string) (map[string][]string, error) {
	return map[string][]string{}, applier.localError
}

func (applier *forceCleanupApplier) DeleteBackupRepositories(context.Context, string, string, []string) ([]string, error) {
	return nil, applier.localError
}

func (applier *forceCleanupApplier) DeleteBackupObjectsByNamePrefix(context.Context, string, string, string) ([]string, error) {
	applier.remoteCalls++
	return nil, nil
}

func TestProtectionCleanupMissingBucketNormalAndForce(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		force        bool
		localFailure bool
		wantStatus   string
	}{
		{"normal", false, false, protocol.MessageAgentTaskFailed},
		{"force", true, false, protocol.MessageAgentTaskCompleted},
		{"local failure remains fatal", true, true, protocol.MessageAgentTaskFailed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			accepted := make(chan *websocket.Conn, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
				if err == nil {
					accepted <- connection
				}
			}))
			defer server.Close()
			connection, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			peer := <-accepted
			defer peer.Close()
			_ = peer.SetReadDeadline(time.Now().Add(3 * time.Second))
			state := t.TempDir()
			box, err := newEventOutbox(state)
			if err != nil {
				t.Fatal(err)
			}
			applier := &forceCleanupApplier{preflightError: minio.ErrorResponse{Code: "NoSuchBucket", Message: "bucket missing"}}
			if scenario.localFailure {
				applier.localError = context.Canceled
			}
			client := &Client{cfg: config.Config{StateDir: state}, conn: connection, outbox: box, applier: applier, logger: slog.Default()}
			task := protocol.TaskDispatchPayload{TaskID: "cleanup", CommandID: "command", Type: "protection-cleanup", ProtectionCleanup: &protocol.ProtectionCleanupCommand{PlanID: "plan", Namespace: "agent", CleanupMode: "source", Force: scenario.force, CleanupObjectStorage: true, StorageRepo: "store", SourceNamespaces: []string{"demo"}, BackupNamePrefix: "hcdr-plan", RestorePoints: []protocol.RetentionRestorePoint{{ID: "point", VeleroBackupName: "backup"}}}}
			client.executeProtectionCleanupTask(task)
			for {
				var message protocol.Message[map[string]any]
				if err := peer.ReadJSON(&message); err != nil {
					t.Fatal(err)
				}
				if message.Type == protocol.MessageAgentTaskCompleted || message.Type == protocol.MessageAgentTaskFailed {
					if message.Type != scenario.wantStatus {
						t.Fatalf("unexpected result: %+v", message)
					}
					if message.Type == protocol.MessageAgentTaskCompleted {
						result := message.Payload["velero"].(map[string]any)
						if result["remoteDataMayRemain"] != true {
							t.Fatal("missing warning about remote data")
						}
					}
					break
				}
			}
			if applier.remoteCalls != 0 {
				t.Fatal("missing bucket must not be accessed repeatedly")
			}
			if !scenario.force && len(applier.deleted) != 0 {
				t.Fatal("normal preflight failure must not delete local records")
			}
		})
	}
}
