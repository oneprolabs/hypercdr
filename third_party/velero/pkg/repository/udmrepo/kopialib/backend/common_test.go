/*
Copyright the Velero contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/repo/content"
	"github.com/kopia/kopia/repo/encryption"
	"github.com/kopia/kopia/repo/format"
	"github.com/kopia/kopia/repo/hashing"
	"github.com/kopia/kopia/repo/splitter"
	"github.com/stretchr/testify/assert"

	"github.com/vmware-tanzu/velero/pkg/repository/udmrepo"
)

func TestPersistentCacheContentIsolation(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		path        string
		drill       string
		wantContent bool
	}{
		{"index only", "/var/cache/hypercdr/repository", "false", false},
		{"index with Drill enabled", "/var/cache/hypercdr/repository", "true", false},
		{"Drill enabled", "/var/cache/hypercdr/drill-data/repository", "true", true},
		{"Drill disabled", "/var/cache/hypercdr/drill-data/repository", "false", false},
		{"similar directory", "/var/cache/hypercdr/drill-data-other/repository", "true", false},
		{"temporary cache", "/tmp/kopia-cache", "false", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("HCDR_DRILL_DATA_CACHE", scenario.drill)
			t.Setenv("HCDR_DRILL_DATA_CACHE_PATH", "/var/cache/hypercdr/drill-data")
			t.Setenv("HCDR_DRILL_DATA_CACHE_BUDGET_BYTES", "64424509440")
			options := SetupConnectOptions(t.Context(), udmrepo.RepoOptions{StorageOptions: map[string]string{udmrepo.StoreOptionCacheDir: scenario.path}}).CachingOptions
			assert.Equal(t, scenario.wantContent, options.ContentCacheSizeBytes > 0)
			assert.Equal(t, scenario.wantContent, options.ContentCacheSizeLimitBytes > 0)
			assert.Positive(t, options.MetadataCacheSizeBytes)
			assert.Positive(t, options.MetadataCacheSizeLimitBytes)
			assert.Equal(t, scenario.path, options.CacheDirectory)
			configPath := filepath.Join(t.TempDir(), "repository.config")
			if err := os.WriteFile(configPath, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetCachingOptions(t.Context(), configPath, &options); err != nil {
				t.Fatal(err)
			}
			saved, err := repo.GetCachingOptions(t.Context(), configPath)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, options.ContentCacheSizeBytes, saved.ContentCacheSizeBytes)
			assert.Equal(t, options.MetadataCacheSizeBytes, saved.MetadataCacheSizeBytes)
			assert.Equal(t, scenario.path, saved.CacheDirectory)
		})
	}
}

func TestSetupNewRepositoryOptions(t *testing.T) {
	testCases := []struct {
		name     string
		flags    map[string]string
		expected repo.NewRepositoryOptions
	}{
		{
			name: "with hash algo",
			flags: map[string]string{
				udmrepo.StoreOptionGenHashAlgo: "fake-hash",
			},
			expected: repo.NewRepositoryOptions{
				BlockFormat: format.ContentFormat{
					Hash:       "fake-hash",
					Encryption: encryption.DefaultAlgorithm,
				},
				ObjectFormat: format.ObjectFormat{
					Splitter: splitter.DefaultAlgorithm,
				},
			},
		},
		{
			name: "with encrypt algo",
			flags: map[string]string{
				udmrepo.StoreOptionGenEncryptAlgo: "fake-encrypt",
			},
			expected: repo.NewRepositoryOptions{
				BlockFormat: format.ContentFormat{
					Hash:       hashing.DefaultAlgorithm,
					Encryption: "fake-encrypt",
				},
				ObjectFormat: format.ObjectFormat{
					Splitter: splitter.DefaultAlgorithm,
				},
			},
		},
		{
			name: "with splitter algo",
			flags: map[string]string{
				udmrepo.StoreOptionGenSplitAlgo: "fake-splitter",
			},
			expected: repo.NewRepositoryOptions{
				BlockFormat: format.ContentFormat{
					Hash:       hashing.DefaultAlgorithm,
					Encryption: encryption.DefaultAlgorithm,
				},
				ObjectFormat: format.ObjectFormat{
					Splitter: "fake-splitter",
				},
			},
		},
		{
			name: "with retention algo",
			flags: map[string]string{
				udmrepo.StoreOptionGenRetentionMode: "fake-retention-mode",
			},
			expected: repo.NewRepositoryOptions{
				BlockFormat: format.ContentFormat{
					Hash:       hashing.DefaultAlgorithm,
					Encryption: encryption.DefaultAlgorithm,
				},
				ObjectFormat: format.ObjectFormat{
					Splitter: splitter.DefaultAlgorithm,
				},
				RetentionMode: "fake-retention-mode",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := SetupNewRepositoryOptions(t.Context(), tc.flags)
			assert.Equal(t, tc.expected, ret)
		})
	}
}

func TestSetupConnectOptions(t *testing.T) {
	defaultCacheOption := content.CachingOptions{
		ContentCacheSizeBytes:       3200 << 20,
		MetadataCacheSizeBytes:      800 << 20,
		ContentCacheSizeLimitBytes:  4000 << 20,
		MetadataCacheSizeLimitBytes: 1000 << 20,
		MaxListCacheDuration:        content.DurationSeconds(time.Duration(30) * time.Second),
	}

	testCases := []struct {
		name        string
		repoOptions udmrepo.RepoOptions
		expected    repo.ConnectOptions
	}{
		{
			name: "with domain",
			repoOptions: udmrepo.RepoOptions{
				GeneralOptions: map[string]string{
					udmrepo.GenOptionOwnerDomain: "fake-domain",
				},
			},
			expected: repo.ConnectOptions{
				CachingOptions: defaultCacheOption,
				ClientOptions: repo.ClientOptions{
					Hostname: "fake-domain",
				},
			},
		},
		{
			name: "with username",
			repoOptions: udmrepo.RepoOptions{
				GeneralOptions: map[string]string{
					udmrepo.GenOptionOwnerName: "fake-user",
				},
			},
			expected: repo.ConnectOptions{
				CachingOptions: defaultCacheOption,
				ClientOptions: repo.ClientOptions{
					Username: "fake-user",
				},
			},
		},
		{
			name: "with wrong readonly",
			repoOptions: udmrepo.RepoOptions{
				GeneralOptions: map[string]string{
					udmrepo.StoreOptionGenReadOnly: "fake-bool",
				},
			},
			expected: repo.ConnectOptions{
				CachingOptions: defaultCacheOption,
				ClientOptions:  repo.ClientOptions{},
			},
		},
		{
			name: "with correct readonly",
			repoOptions: udmrepo.RepoOptions{
				GeneralOptions: map[string]string{
					udmrepo.StoreOptionGenReadOnly: "true",
				},
			},
			expected: repo.ConnectOptions{
				CachingOptions: defaultCacheOption,
				ClientOptions: repo.ClientOptions{
					ReadOnly: true,
				},
			},
		},
		{
			name: "with description",
			repoOptions: udmrepo.RepoOptions{
				Description: "fake-description",
			},
			expected: repo.ConnectOptions{
				CachingOptions: defaultCacheOption,
				ClientOptions: repo.ClientOptions{
					Description: "fake-description",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := SetupConnectOptions(t.Context(), tc.repoOptions)
			assert.Equal(t, tc.expected, ret)
		})
	}
}
