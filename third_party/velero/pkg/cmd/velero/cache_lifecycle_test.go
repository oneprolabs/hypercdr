package velero

import "testing"

func TestCacheLifecycleCommandRequiresPathAndIsolationID(t *testing.T) {
	for _, arguments := range [][]string{nil, {"/cache"}, {"/cache", "isolation", "extra"}} {
		called := false
		command := newCacheLifecycleCommand("cache-test", func(root, isolationID string) error {
			called = true
			return nil
		})
		command.SetArgs(arguments)
		if err := command.Execute(); err == nil || called {
			t.Fatalf("accepted invalid arguments: %v", arguments)
		}
	}
	called := false
	command := newCacheLifecycleCommand("cache-test", func(root, isolationID string) error {
		called = true
		if root != "/cache" || isolationID != "isolation" {
			t.Fatalf("unexpected command arguments: %s %s", root, isolationID)
		}
		return nil
	})
	command.SetArgs([]string{"/cache", "isolation"})
	if err := command.Execute(); err != nil || !called {
		t.Fatalf("valid command failed: %v", err)
	}
}
