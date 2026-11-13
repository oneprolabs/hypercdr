package backend

import (
	"bytes"
	"io"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/blob/filesystem"
	"github.com/kopia/kopia/repo/cachemetrics"
	"github.com/kopia/kopia/repo/object"
	"github.com/stretchr/testify/require"
	"github.com/vmware-tanzu/velero/pkg/repository/udmrepo"
)

func TestCacheStatisticsCountLogicalBytesAcrossColdWarmAndDisabledReads(t *testing.T) {
	for _, size := range []int{128 << 10, 32 << 20} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmtCacheScenario(size, enabled), func(t *testing.T) {
				ctx := t.Context()
				t.Setenv("HCDR_CACHE_PERSISTENCE", "true")
				t.Setenv("HCDR_DRILL_DATA_CACHE", map[bool]string{true: "true", false: "false"}[enabled])
				t.Setenv("HCDR_DRILL_DATA_CACHE_PATH", "/var/cache/hypercdr/drill-data")
				storage, err := filesystem.New(ctx, &filesystem.Options{Path: t.TempDir()}, true)
				require.NoError(t, err)
				defer storage.Close(ctx)
				password := "integration-test"
				require.NoError(t, repo.Initialize(ctx, storage, &repo.NewRepositoryOptions{}, password))
				config := filepath.Join(t.TempDir(), "repository.config")
				require.NoError(t, repo.Connect(ctx, config, storage, password, &repo.ConnectOptions{}))
				repository, err := repo.Open(ctx, config, password, nil)
				require.NoError(t, err)
				writeCtx, writer, err := repository.NewWriter(ctx, repo.WriteSessionOptions{})
				require.NoError(t, err)
				data := make([]byte, size)
				_, err = rand.New(rand.NewSource(123)).Read(data)
				require.NoError(t, err)
				for index := 0; index < len(data); index += 2 {
					data[index] = 0
				}
				output := writer.NewObjectWriter(writeCtx, object.WriterOptions{Compressor: "zstd"})
				_, err = output.Write(data)
				require.NoError(t, err)
				objectID, err := output.Result()
				require.NoError(t, err)
				require.NoError(t, writer.Flush(writeCtx))
				require.NoError(t, writer.Close(writeCtx))
				require.NoError(t, repository.Close(ctx))
				options := SetupConnectOptions(ctx, udmrepo.RepoOptions{StorageOptions: map[string]string{udmrepo.StoreOptionCacheDir: "/var/cache/hypercdr/repository"}})
				if enabled {
					options = SetupConnectOptions(ctx, udmrepo.RepoOptions{StorageOptions: map[string]string{udmrepo.StoreOptionCacheDir: "/var/cache/hypercdr/drill-data/repository"}})
				}
				options.CacheDirectory = t.TempDir()
				for round := 0; round < 2; round++ {
					observer := cachemetrics.New(true, enabled)
					taskCtx := cachemetrics.WithObserver(ctx, observer)
					if round == 0 {
						require.NoError(t, repo.Connect(taskCtx, config, storage, password, &options))
					}
					repository, err = repo.Open(taskCtx, config, password, nil)
					require.NoError(t, err)
					reader, err := repository.OpenObject(cachemetrics.WithFileRead(taskCtx, true), objectID)
					require.NoError(t, err)
					restored, err := io.ReadAll(reader)
					require.NoError(t, err)
					require.True(t, bytes.Equal(data, restored))
					require.NoError(t, reader.Close())
					require.NoError(t, repository.Close(taskCtx))
					stats := observer.Snapshot()
					require.Equal(t, int64(len(data)), stats.DataReadBytes)
					if round == 1 && enabled {
						require.Equal(t, int64(len(data)), stats.DataHitBytes)
					} else {
						require.Zero(t, stats.DataHitBytes)
					}
					require.LessOrEqual(t, stats.IndexHitBytes, stats.IndexReadBytes)
					require.Positive(t, stats.IndexReadBytes)
					if round == 0 {
						require.Zero(t, stats.IndexHitBytes)
					} else {
						require.Equal(t, stats.IndexReadBytes, stats.IndexHitBytes)
					}
					t.Logf("size=%d enabled=%v round=%d stats=%+v", size, enabled, round, stats)
				}
			})
		}
	}
}

func fmtCacheScenario(size int, enabled bool) string {
	if size > 1<<20 {
		if enabled {
			return "chunked-enabled"
		}
		return "chunked-disabled"
	}
	if enabled {
		return "small-enabled"
	}
	return "small-disabled"
}
