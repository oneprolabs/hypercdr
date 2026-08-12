package wsclient

import (
	"context"
	"errors"
	"testing"
)

func TestRetryProtectionCleanupRetriesTransientKubernetesError(t *testing.T) {
	attempts := 0
	result, err := retryProtectionCleanup(context.Background(), nil, "restore objects", 3, 0, func(context.Context) ([]string, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("etcdserver: request timed out")
		}
		return []string{"restores/demo"}, nil
	})
	if err != nil || attempts != 3 || len(result) != 1 {
		t.Fatalf("result=%v attempts=%d err=%v", result, attempts, err)
	}
}

func TestRetryProtectionCleanupDoesNotRetryPermanentError(t *testing.T) {
	attempts := 0
	_, err := retryProtectionCleanup(context.Background(), nil, "restore objects", 3, 0, func(context.Context) ([]string, error) {
		attempts++
		return nil, errors.New("access denied")
	})
	if err == nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
