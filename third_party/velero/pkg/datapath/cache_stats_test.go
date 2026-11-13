package datapath

import (
	"context"
	"testing"

	"github.com/kopia/kopia/repo/cachemetrics"
	"github.com/vmware-tanzu/velero/pkg/uploader"
)

func TestCacheStatisticsAreFlushedWithoutNewTransferProgress(t *testing.T) {
	observer := cachemetrics.New(true, true)
	ctx := cachemetrics.WithFileRead(cachemetrics.WithObserver(context.Background(), observer), true)
	var observed []uploader.Progress
	fs := &fileSystemBR{cacheObserver: observer, callbacks: Callbacks{OnProgress: func(_ context.Context, _, _ string, progress *uploader.Progress) {
		observed = append(observed, *progress)
	}}}
	cachemetrics.RecordData(ctx, 100, 25)
	fs.UpdateProgress(&uploader.Progress{TotalBytes: 200, BytesDone: 100})
	cachemetrics.RecordData(ctx, 100, 100)
	fs.flushCacheProgress()
	if len(observed) != 2 || observed[0].CacheStats.DataHitBytes != 25 || observed[1].CacheStats.DataHitBytes != 125 || observed[1].CacheStats.DataReadBytes != 200 {
		t.Fatalf("%+v", observed)
	}
	if observed[1].TotalBytes != 200 || observed[1].BytesDone != 100 {
		t.Fatal("transfer progress was changed by cache reporting")
	}
}
