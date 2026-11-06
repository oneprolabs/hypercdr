package wsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type forbiddenObservationStatsReader struct {
	backupSizeStatsReader
}

func (forbiddenObservationStatsReader) GetBackupObjectStats(context.Context, string, string, string) (kube.BackupObjectStats, error) {
	panic("background observation must not read object storage statistics")
}

func (forbiddenObservationStatsReader) GetBackupVolumeInfoStats(context.Context, string, string, string) (kube.BackupVolumeInfoStats, error) {
	panic("background observation must not read volume info from object storage")
}

func (forbiddenObservationStatsReader) GetPlanObjectStorageStats(context.Context, string, string, string, []string) (kube.PlanObjectStorageStats, error) {
	panic("background observation must not scan the plan repository")
}

func TestCompletedBackupObservationNeverReadsObjectStorage(t *testing.T) {
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
	if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		box, err := newEventOutbox(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		client := &Client{
			conn: connection, outbox: box, statsReader: forbiddenObservationStatsReader{},
			backupSamples: map[string][]volumeProgressSample{}, backupEvents: map[string]string{},
		}
		backup := kube.VeleroBackupSummary{Name: "backup", Namespace: "hypercdr-agent", Phase: "Completed", ResourceVersion: "1", StorageLocation: "default"}
		if err := client.sendVeleroBackupEvent(backup); err != nil {
			t.Fatal(err)
		}
		var event protocol.Message[protocol.VeleroEventPayload]
		if err := peer.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Payload.Phase != "Completed" {
			t.Fatalf("unexpected observation: %+v", event.Payload)
		}
		if _, present := event.Payload.Velero["restorePointSize"]; present {
			t.Fatal("observation must not publish final size measurements")
		}
		for repeat := 0; repeat < 3; repeat++ {
			if err := client.sendVeleroBackupEvent(backup); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type backupSizeStatsReader struct {
	metadataBytes int64
	volumeBytes   int64
}

func (r backupSizeStatsReader) GetBackupObjectStats(context.Context, string, string, string) (kube.BackupObjectStats, error) {
	return kube.BackupObjectStats{MetadataPackageBytes: r.metadataBytes}, nil
}

func (r backupSizeStatsReader) GetBackupVolumeInfoStats(context.Context, string, string, string) (kube.BackupVolumeInfoStats, error) {
	return kube.BackupVolumeInfoStats{VolumeBytes: r.volumeBytes, Accurate: true}, nil
}

func (backupSizeStatsReader) GetPlanObjectStorageStats(context.Context, string, string, string, []string) (kube.PlanObjectStorageStats, error) {
	return kube.PlanObjectStorageStats{}, nil
}

func (backupSizeStatsReader) GetRestoreResultSummary(context.Context, string, string, string) (kube.RestoreResultSummary, error) {
	return kube.RestoreResultSummary{}, nil
}

func TestAttachBackupSizeStatsPreservesKnownZeroIncrementalUpload(t *testing.T) {
	const (
		metadataBytes = int64(24_001)
		volumeBytes   = int64(210_427_776)
	)
	client := &Client{statsReader: backupSizeStatsReader{
		metadataBytes: metadataBytes,
		volumeBytes:   volumeBytes,
	}}
	payload := map[string]any{
		"storageLocation": "default",
		"volumeProgress": map[string]any{
			"volumeBytes":      volumeBytes,
			"incrementalBytes": int64(0),
			"incrementalCount": int64(1),
		},
	}

	client.attachBackupSizeStats(context.Background(), kube.AppliedObject{
		Kind:      "Backup",
		Namespace: "hypercdr-agent",
		Name:      "backup-1",
	}, payload)

	size := wsMapFromAny(payload["restorePointSize"])
	if got := int64FromAny(size["totalBytes"]); got != metadataBytes+volumeBytes {
		t.Fatalf("totalBytes = %d, want %d", got, metadataBytes+volumeBytes)
	}
	if got := int64FromAny(size["uploadedVolumeBytes"]); got != 0 {
		t.Fatalf("uploadedVolumeBytes = %d, want 0", got)
	}
	if got := int64FromAny(size["uploadedBytes"]); got != metadataBytes {
		t.Fatalf("uploadedBytes = %d, want %d", got, metadataBytes)
	}
}
