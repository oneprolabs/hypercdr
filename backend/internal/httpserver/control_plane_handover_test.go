package httpserver

import "testing"

func TestSupportedControlPlaneHandoverAction(t *testing.T) {
	for _, action := range []string{"confirm", "commit", "rollback", "cleanup"} {
		if !supportedControlPlaneHandoverAction(action) {
			t.Fatalf("expected action %q to be supported", action)
		}
	}
	for _, action := range []string{"", "prepare", "delete"} {
		if supportedControlPlaneHandoverAction(action) {
			t.Fatalf("expected action %q to be rejected", action)
		}
	}
}
