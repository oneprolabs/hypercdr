package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestNextPolicyFireAtUsesPlatformLocation(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		policy store.Policy
		after  time.Time
		want   time.Time
	}{
		{
			name:   "daily later today",
			policy: store.Policy{ScheduleType: "daily", Hour: 9, Minute: 0},
			after:  time.Date(2026, 7, 20, 0, 30, 0, 0, time.UTC),
			want:   time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC),
		},
		{
			name:   "daily next day after local time passed",
			policy: store.Policy{ScheduleType: "daily", Hour: 9, Minute: 0},
			after:  time.Date(2026, 7, 20, 1, 30, 0, 0, time.UTC),
			want:   time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC),
		},
		{
			name:   "weekly uses local weekday",
			policy: store.Policy{ScheduleType: "weekly", WeekDay: int(time.Monday), Hour: 9, Minute: 0},
			after:  time.Date(2026, 7, 20, 0, 30, 0, 0, time.UTC),
			want:   time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC),
		},
		{
			name:   "weekly advances seven days after local time passed",
			policy: store.Policy{ScheduleType: "weekly", WeekDay: int(time.Monday), Hour: 9, Minute: 0},
			after:  time.Date(2026, 7, 20, 1, 30, 0, 0, time.UTC),
			want:   time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC),
		},
		{
			name:   "monthly clamps to last local day",
			policy: store.Policy{ScheduleType: "monthly", MonthDay: 31, Hour: 9, Minute: 0},
			after:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			want:   time.Date(2026, 4, 30, 1, 0, 0, 0, time.UTC),
		},
		{
			name:   "monthly preserves leap day",
			policy: store.Policy{ScheduleType: "monthly", MonthDay: 29, Hour: 9, Minute: 0},
			after:  time.Date(2028, 2, 1, 0, 0, 0, 0, time.UTC),
			want:   time.Date(2028, 2, 29, 1, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextPolicyFireAtInLocation(test.policy, test.after, shanghai); !got.Equal(test.want) {
				t.Fatalf("next fire = %s, want %s", got, test.want)
			}
		})
	}
}

func TestNextPolicyFireAtFollowsDaylightSavingOffset(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	policy := store.Policy{ScheduleType: "daily", Hour: 9, Minute: 0}
	after := time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)
	want := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	if got := nextPolicyFireAtInLocation(policy, after, newYork); !got.Equal(want) {
		t.Fatalf("next fire across DST = %s, want %s", got, want)
	}
}

func TestScheduleMatchesPolicyDetectsLegacyUTCTime(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := store.Policy{ScheduleType: "daily", Hour: 9, Minute: 0}
	correct := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	legacyUTC := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	if !scheduleMatchesPolicy(correct, policy, shanghai) {
		t.Fatal("correct platform-local schedule was not recognized")
	}
	if scheduleMatchesPolicy(legacyUTC, policy, shanghai) {
		t.Fatal("legacy UTC wall-clock schedule was not detected")
	}
}
