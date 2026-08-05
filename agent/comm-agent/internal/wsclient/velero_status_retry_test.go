package wsclient

import (
	"errors"
	"testing"
	"time"
)

func TestShouldRetryVeleroStatusReadDuringTransientAPIOutage(t *testing.T) {
	now := time.Date(2026, 8, 5, 5, 46, 9, 0, time.UTC)
	err := errors.New(`Get "https://10.96.0.1:443/apis/velero.io/v1/restores/test": dial tcp 10.96.0.1:443: connect: connection refused`)
	if !shouldRetryVeleroStatusRead(err, now, now.Add(-time.Minute), now.Add(10*time.Minute)) {
		t.Fatal("expected connection refusal inside grace period to be retried")
	}
}

func TestShouldRetryVeleroStatusReadStopsAfterGraceOrTaskDeadline(t *testing.T) {
	now := time.Date(2026, 8, 5, 5, 46, 9, 0, time.UTC)
	err := errors.New("connect: connection refused")
	if shouldRetryVeleroStatusRead(err, now, now.Add(-veleroStatusReadRetryGrace), now.Add(time.Minute)) {
		t.Fatal("expected retry grace expiry to stop retries")
	}
	if shouldRetryVeleroStatusRead(err, now, now.Add(-time.Second), now) {
		t.Fatal("expected task deadline to stop retries")
	}
	if shouldRetryVeleroStatusRead(errors.New("forbidden"), now, now.Add(-time.Second), now.Add(time.Minute)) {
		t.Fatal("expected permanent error to fail immediately")
	}
}
