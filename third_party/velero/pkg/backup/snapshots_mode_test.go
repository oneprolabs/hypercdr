package backup

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	kbclient "sigs.k8s.io/controller-runtime/pkg/client"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/features"
	"github.com/vmware-tanzu/velero/pkg/util/boolptr"
)

type unavailableSnapshotClient struct {
	kbclient.Client
	queries int
}

func (client *unavailableSnapshotClient) List(_ context.Context, _ kbclient.ObjectList, _ ...kbclient.ListOption) error {
	client.queries++
	return fmt.Errorf("no matches for kind VolumeSnapshot in snapshot.storage.k8s.io/v1")
}

func TestBackupSnapshotQueriesFollowBackupMode(t *testing.T) {
	features.NewFeatureFlagSet("EnableCSI")
	t.Cleanup(func() { features.NewFeatureFlagSet("") })
	for _, scenario := range []struct {
		name                string
		snapshots, moveData *bool
		queries             int
	}{
		{"filesystem without snapshot API", boolptr.False(), nil, 0},
		{"filesystem with explicit false movement", boolptr.False(), boolptr.False(), 0},
		{"CSI data movement", boolptr.True(), boolptr.True(), 0},
		{"CSI snapshot persistence", boolptr.True(), nil, 2},
		{"legacy unspecified snapshot policy", nil, nil, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := &unavailableSnapshotClient{}
			backup := &velerov1api.Backup{Spec: velerov1api.BackupSpec{SnapshotVolumes: scenario.snapshots, SnapshotMoveData: scenario.moveData}}
			var output bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&output)
			snapshots, contents, classes := GetBackupCSIResources(client, client, backup, logger)
			if client.queries != scenario.queries {
				t.Fatalf("snapshot API queries = %d, want %d", client.queries, scenario.queries)
			}
			if len(snapshots)+len(contents)+len(classes) != 0 {
				t.Fatal("unavailable API returned snapshot resources")
			}
			if scenario.queries == 0 && bytes.Contains(output.Bytes(), []byte("level=error")) {
				t.Fatalf("unused snapshot API caused a backup error: %s", output.String())
			}
			if scenario.queries > 0 && !bytes.Contains(output.Bytes(), []byte("level=error")) {
				t.Fatal("required CSI snapshot persistence errors must remain visible")
			}
		})
	}
}
