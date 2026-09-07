package httpserver

import "testing"

func TestClusterTypesDRCompatible(t *testing.T) {
	tests := []struct {
		source, target string
		want           bool
	}{
		{"native-kubernetes", "native-kubernetes", true},
		{"native-kubernetes", "huaweicloud-cce", true},
		{"huaweicloud-cce", "native-kubernetes", true},
		{"openshift", "openshift", true},
		{"openshift", "native-kubernetes", false},
		{"huaweicloud-cce", "openshift", false},
	}
	for _, test := range tests {
		if got := clusterTypesDRCompatible(test.source, test.target); got != test.want {
			t.Errorf("clusterTypesDRCompatible(%q, %q) = %v, want %v", test.source, test.target, got, test.want)
		}
	}
}
