package store

import (
	"testing"
	"time"
)

func TestRestorePointDisplayNameUsesLocalTime(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("test-local", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	taskCreatedAt := time.Date(2026, time.July, 20, 1, 2, 3, 0, time.UTC)
	got := restorePointDisplayName("", taskCreatedAt, time.Time{})
	if want := "RP-2026-07-20 09:02:03"; got != want {
		t.Fatalf("restorePointDisplayName() = %q, want %q", got, want)
	}
}

func TestRestorePointDisplayNameKeepsExplicitName(t *testing.T) {
	got := restorePointDisplayName("  RP-custom  ", time.Time{}, time.Now())
	if want := "RP-custom"; got != want {
		t.Fatalf("restorePointDisplayName() = %q, want %q", got, want)
	}
}
