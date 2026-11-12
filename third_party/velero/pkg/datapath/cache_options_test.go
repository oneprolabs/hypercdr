package datapath

import "testing"

func TestPersistentCacheOptionsIndependentSwitches(t *testing.T) {
	for _, indexEnabled := range []string{"true", "false"} {
		for _, drillEnabled := range []string{"true", "false"} {
			for _, identity := range []string{"restore/demo", "backup/demo", ""} {
				t.Run(indexEnabled+"/"+drillEnabled+"/"+identity, func(t *testing.T) {
					t.Setenv("HCDR_CACHE_PERSISTENCE", indexEnabled)
					t.Setenv("HCDR_DRILL_DATA_CACHE", drillEnabled)
					t.Setenv("HCDR_DRILL_DATA_CACHE_PATH", "/var/cache/hypercdr/drill-data")
					root, budget, enabled := persistentCacheOptions(identity)
					wantEnabled := identity != "" && (indexEnabled == "true" || (identity == "restore/demo" && drillEnabled == "true"))
					if enabled != wantEnabled {
						t.Fatalf("enabled=%v, want %v", enabled, wantEnabled)
					}
					if !enabled {
						return
					}
					wantRoot, wantBudget := "/var/cache/hypercdr", "HCDR_CACHE_BUDGET_BYTES"
					if identity == "restore/demo" && drillEnabled == "true" {
						wantRoot, wantBudget = "/var/cache/hypercdr/drill-data", "HCDR_DRILL_DATA_CACHE_BUDGET_BYTES"
					}
					if root != wantRoot || budget != wantBudget {
						t.Fatalf("got %s %s, want %s %s", root, budget, wantRoot, wantBudget)
					}
				})
			}
		}
	}
}
