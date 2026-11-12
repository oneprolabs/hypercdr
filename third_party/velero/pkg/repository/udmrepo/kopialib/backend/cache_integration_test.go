package backend

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/blob/filesystem"
	"github.com/kopia/kopia/repo/object"
	"github.com/stretchr/testify/require"

	"github.com/vmware-tanzu/velero/pkg/repository/udmrepo"
)

func TestPersistentBusinessCacheReadsAcrossConnections(t *testing.T) {
	for _, indexEnabled := range []string{"false", "true"} {
		t.Run("index="+indexEnabled, func(t *testing.T) {
			t.Setenv("HCDR_CACHE_PERSISTENCE", indexEnabled)
			for _, drillEnabled := range []string{"false", "true"} {
				t.Run(drillEnabled, func(t *testing.T) {
					ctx := t.Context()
					t.Setenv("HCDR_DRILL_DATA_CACHE", drillEnabled)
					t.Setenv("HCDR_DRILL_DATA_CACHE_PATH", "/var/cache/hypercdr/drill-data")
					t.Setenv("HCDR_DRILL_DATA_CACHE_BUDGET_BYTES", "1073741824")
					storage, err := filesystem.New(ctx, &filesystem.Options{Path: t.TempDir()}, true)
					require.NoError(t, err)
					defer storage.Close(ctx)
					password := "test-repository-password"
					require.NoError(t, repo.Initialize(ctx, storage, &repo.NewRepositoryOptions{}, password))
					configPath := filepath.Join(t.TempDir(), "repository.config")
					require.NoError(t, repo.Connect(ctx, configPath, storage, password, &repo.ConnectOptions{}))
					repository, err := repo.Open(ctx, configPath, password, nil)
					require.NoError(t, err)
					writeContext, writer, err := repository.NewWriter(ctx, repo.WriteSessionOptions{})
					require.NoError(t, err)
					objectWriter := writer.NewObjectWriter(writeContext, object.WriterOptions{})
					data := bytes.Repeat([]byte("business-cache-verification"), 4096)
					_, err = objectWriter.Write(data)
					require.NoError(t, err)
					objectID, err := objectWriter.Result()
					require.NoError(t, err)
					require.NoError(t, writer.Flush(writeContext))
					require.NoError(t, writer.Close(writeContext))
					require.NoError(t, repository.Close(ctx))
					cachePath := "/var/cache/hypercdr/repository"
					if drillEnabled == "true" {
						cachePath = "/var/cache/hypercdr/drill-data/repository"
					}
					options := SetupConnectOptions(ctx, udmrepo.RepoOptions{StorageOptions: map[string]string{udmrepo.StoreOptionCacheDir: cachePath}})
					options.CacheDirectory = t.TempDir()
					require.NoError(t, repo.Connect(ctx, configPath, storage, password, &options))
					repository, err = repo.Open(ctx, configPath, password, nil)
					require.NoError(t, err)
					reader, err := repository.OpenObject(ctx, objectID)
					require.NoError(t, err)
					restored, err := io.ReadAll(reader)
					require.NoError(t, err)
					require.Equal(t, data, restored)
					require.NoError(t, reader.Close())
					contentIDs, err := repository.VerifyObject(ctx, objectID)
					require.NoError(t, err)
					require.NotEmpty(t, contentIDs)
					info, err := repository.ContentInfo(ctx, contentIDs[0])
					require.NoError(t, err)
					require.NoError(t, repository.Close(ctx))
					require.NoError(t, storage.DeleteBlob(ctx, info.PackBlobID))
					repository, err = repo.Open(ctx, configPath, password, nil)
					require.NoError(t, err)
					defer repository.Close(ctx)
					reader, readErr := repository.OpenObject(ctx, objectID)
					if readErr == nil {
						restored, readErr = io.ReadAll(reader)
						require.NoError(t, reader.Close())
					}
					if drillEnabled == "true" {
						require.NoError(t, readErr)
						require.Equal(t, data, restored)
					} else {
						require.Error(t, readErr)
					}
				})
			}
		})
	}
}
