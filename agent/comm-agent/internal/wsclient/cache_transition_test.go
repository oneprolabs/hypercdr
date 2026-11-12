package wsclient

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type cacheTransitionFake struct {
	calls    []string
	failures map[string]error
}

func TestRegistrationCacheConfigurationDefersToDurableTransition(t *testing.T) {
	ledger, err := newTaskLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{ledger: ledger}
	if client.cacheTransitionPending() {
		t.Fatal("empty ledger blocked registration configuration")
	}
	task := protocol.TaskDispatchPayload{TaskID: "cache-transition", Type: "cache-settings", CacheSettings: &protocol.CacheSettingsCommand{Transition: &protocol.CacheSettingsTransition{IsolationID: "fixed"}}}
	if err := ledger.recordTask(task.TaskID, "command", "", task, taskLedgerObject{Kind: "CacheSettings", Namespace: "namespace", Name: task.TaskID}); err != nil {
		t.Fatal(err)
	}
	if !client.cacheTransitionPending() {
		t.Fatal("registration could overwrite an unfinished transition")
	}
	if err := ledger.markTerminalAcked(task.TaskID); err != nil {
		t.Fatal(err)
	}
	if client.cacheTransitionPending() {
		t.Fatal("acknowledged transition still blocked registration configuration")
	}
}

func (runtime *cacheTransitionFake) ConfigureCache(ctx context.Context, namespace, rootPath, indexPath, drillPath string, indexEnabled bool, indexMaxGiB int, drillEnabled bool, drillMaxGiB int, clean bool) error {
	name := "configure:" + rootPath
	runtime.calls = append(runtime.calls, name)
	return runtime.failures[name]
}

func (runtime *cacheTransitionFake) ValidateCacheDirectoryTransition(context.Context, string, string, []string, []string) error {
	runtime.calls = append(runtime.calls, "validate")
	return runtime.failures["validate"]
}

func (runtime *cacheTransitionFake) RunCacheDirectoryStage(ctx context.Context, namespace, operation, isolationID string, paths, nodes []string) error {
	runtime.calls = append(runtime.calls, operation)
	return runtime.failures[operation]
}

func TestCacheTransitionFailureAndRollbackOrdering(t *testing.T) {
	command := protocol.CacheSettingsCommand{RootPath: "/new", Transition: &protocol.CacheSettingsTransition{Previous: protocol.CacheConfigurationSnapshot{RootPath: "/old"}}}
	failure := errors.New("validation failure")
	for _, scenario := range []struct {
		name     string
		failures map[string]error
		expected []string
		safe     bool
		failed   bool
	}{
		{"success", nil, []string{"validate", "configure:/new", "isolate"}, true, false},
		{"unsupported", map[string]error{"validate": failure}, []string{"validate"}, true, true},
		{"apply failed", map[string]error{"configure:/new": failure}, []string{"validate", "configure:/new", "configure:/old"}, true, true},
		{"rename failed", map[string]error{"isolate": kube.ErrCacheDirectoryStageFailed}, []string{"validate", "configure:/new", "isolate", "rollback", "configure:/old"}, true, true},
		{"unknown isolation state", map[string]error{"isolate": context.DeadlineExceeded}, []string{"validate", "configure:/new", "isolate"}, false, true},
		{"directory rollback failed", map[string]error{"isolate": kube.ErrCacheDirectoryStageFailed, "rollback": failure}, []string{"validate", "configure:/new", "isolate", "rollback"}, false, true},
		{"configuration rollback failed", map[string]error{"isolate": kube.ErrCacheDirectoryStageFailed, "configure:/old": failure}, []string{"validate", "configure:/new", "isolate", "rollback", "configure:/old"}, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime := &cacheTransitionFake{failures: scenario.failures}
			safe, err := applyCacheTransition(context.Background(), runtime, command)
			if safe != scenario.safe || (err != nil) != scenario.failed || !reflect.DeepEqual(runtime.calls, scenario.expected) {
				t.Fatalf("safe=%v err=%v calls=%v", safe, err, runtime.calls)
			}
		})
	}
}

func TestCacheTransitionRestartResumesRollbackWithoutApplyingNewSettings(t *testing.T) {
	directory := t.TempDir()
	ledger, err := newTaskLedger(directory)
	if err != nil {
		t.Fatal(err)
	}
	command := protocol.CacheSettingsCommand{RootPath: "/new", Transition: &protocol.CacheSettingsTransition{Previous: protocol.CacheConfigurationSnapshot{RootPath: "/old"}}}
	task := protocol.TaskDispatchPayload{TaskID: "cache-task", Type: "cache-settings", CacheSettings: &command}
	if err := ledger.recordTask(task.TaskID, "command", "", task, taskLedgerObject{Kind: "CacheSettings", Name: task.TaskID}); err != nil {
		t.Fatal(err)
	}
	runtime := &cacheTransitionFake{failures: map[string]error{"isolate": kube.ErrCacheDirectoryStageFailed, "rollback": errors.New("node unavailable")}}
	safe, err := resumeCacheTransition(context.Background(), runtime, command, cacheTransitionState{}, func(state cacheTransitionState) error { return ledger.saveCacheTransition(task.TaskID, state) })
	if safe || err == nil {
		t.Fatal("failed rollback released configuration gate")
	}
	reloaded, err := newTaskLedger(directory)
	if err != nil {
		t.Fatal(err)
	}
	state, err := reloaded.cacheTransition(task.TaskID)
	if err != nil || state.Phase != "rollback-directories" {
		t.Fatalf("rollback phase lost: %+v %v", state, err)
	}
	runtime.calls = nil
	delete(runtime.failures, "rollback")
	safe, err = resumeCacheTransition(context.Background(), runtime, command, state, func(state cacheTransitionState) error { return reloaded.saveCacheTransition(task.TaskID, state) })
	if !safe || err == nil || !reflect.DeepEqual(runtime.calls, []string{"rollback", "configure:/old"}) {
		t.Fatalf("unsafe restart: safe=%v error=%v calls=%v", safe, err, runtime.calls)
	}
}
