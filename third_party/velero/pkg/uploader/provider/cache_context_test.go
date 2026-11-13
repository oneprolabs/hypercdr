package provider

import (
	"context"
	"testing"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/cachemetrics"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/uploader"
	"github.com/vmware-tanzu/velero/pkg/uploader/kopia"
)

type cacheContextUpdater struct{}

func (cacheContextUpdater) UpdateProgress(*uploader.Progress) {}

func TestRestorePreservesTaskCacheObserverWithoutChangingCancelChannel(t *testing.T) {
	original := RestoreFunc
	t.Cleanup(func() { RestoreFunc = original })
	observer := cachemetrics.New(true, true)
	ctx, cancel := context.WithCancel(cachemetrics.WithObserver(context.Background(), observer))
	defer cancel()
	called := false
	RestoreFunc = func(ctx context.Context, _ repo.RepositoryWriter, _ *kopia.Progress, _, _ string, _ uploader.PersistentVolumeMode, _ map[string]string, _ logrus.FieldLogger, cancelled chan struct{}) (int64, int32, error) {
		called = true
		if cachemetrics.FromContext(ctx) != observer {
			t.Fatal("per-task cache context was lost")
		}
		if ctx.Done() != nil || cancelled == nil {
			t.Fatal("restore cancellation behavior changed")
		}
		return 0, 0, nil
	}
	provider := &kopiaProvider{log: logrus.New()}
	if _, err := provider.RunRestore(ctx, "snapshot", "/unused", uploader.PersistentVolumeFilesystem, nil, cacheContextUpdater{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("restore was not called")
	}
}
