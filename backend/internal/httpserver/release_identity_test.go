package httpserver

import "testing"

func TestReleaseNumberingEraAndEdition(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
		comparable  bool
	}{
		{"ce-v0.0.0", "1.0.142.20261015", 1, true},
		{"1.0.142.20261015", "ce-v0.0.0", -1, true},
		{"ee-v0.0.0", "v1.0.142.20261015-ent.2", 1, true},
		{"ce-v0.0.10", "ce-v0.0.2", 1, true},
		{"ee-v0.0.0", "ce-v0.0.0", 0, false},
		{"ce-v0.0.0", "ce-v0.0.0", 0, true},
	} {
		got, ok := compareNumericVersions(test.left, test.right)
		if ok != test.comparable || got != test.want {
			t.Errorf("%s / %s: got %d,%v", test.left, test.right, got, ok)
		}
	}
}
func TestReleaseCatalogKeepsEditionBoundary(t *testing.T) {
	for _, tag := range []string{"ce-v0.0.0", "v1.0.142.20261015"} {
		if !releaseTagMatchesEdition(tag, "community") || releaseTagMatchesEdition(tag, "enterprise") {
			t.Errorf("Community tag misclassified: %s", tag)
		}
	}
	for _, tag := range []string{"ee-v0.0.0", "v1.0.142.20261015-ent.2"} {
		if !releaseTagMatchesEdition(tag, "enterprise") || releaseTagMatchesEdition(tag, "community") {
			t.Errorf("Enterprise tag misclassified: %s", tag)
		}
	}
}

func TestUpgradeRejectsOtherEditionEvenWithChangedDigest(t *testing.T) {
	if upgradeTargetIsNewer("ce-v0.0.0", "ee-v0.0.1", true) {
		t.Fatal("cross-edition upgrade offered")
	}
}
