package store

import (
	"testing"
	"time"
)

func TestRetentionRollingPeriods(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	points := []RestorePoint{}
	for day := 22; day <= 26; day++ {
		for ordinal := 1; ordinal <= 2; ordinal++ {
			points = append(points, RestorePoint{ID: time.Date(2026, 10, day, ordinal, 0, 0, 0, time.UTC).Format(time.RFC3339), Status: "available", TaskCreatedAt: time.Date(2026, 10, day, ordinal, 0, 0, 0, time.UTC)})
		}
	}
	selected, err := RetentionSelections(points, []RetentionRule{{Period: "daily", Ordinal: 1, Keep: 3}}, "Asia/Shanghai", now)
	if err != nil || len(selected) != 3 {
		t.Fatalf("selected=%v err=%v", selected, err)
	}
	for _, day := range []int{24, 25, 26} {
		key := time.Date(2026, 10, day, 1, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if len(selected[key]) != 1 {
			t.Fatalf("missing day %d", day)
		}
	}
	expired, _ := RetentionSelections(points, []RetentionRule{{Period: "daily", Ordinal: 1, Keep: 3}}, "Asia/Shanghai", now.AddDate(0, 0, 1))
	if len(expired) != 2 {
		t.Fatalf("missing day must not backfill: %v", expired)
	}
}

func TestRetentionDeletedOrdinalIsNotReassigned(t *testing.T) {
	now := time.Now().UTC()
	points := []RestorePoint{{ID: "first", Status: "deleted", TaskCreatedAt: now.Add(-time.Minute)}, {ID: "second", Status: "available", TaskCreatedAt: now}}
	selected, err := RetentionSelections(points, []RetentionRule{{Period: "daily", Ordinal: 1, Keep: 3}}, "UTC", now)
	if err != nil || len(selected) != 0 {
		t.Fatalf("deleted first must not promote second: %v %v", selected, err)
	}
}

func TestRetentionCalendarBoundariesAndOverlap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	point := RestorePoint{ID: "point", Status: "available", TaskCreatedAt: now.Add(-time.Hour)}
	rules := []RetentionRule{{Period: "daily", Ordinal: 1, Keep: 1}, {Period: "weekly", Ordinal: 1, Keep: 1}, {Period: "monthly", Ordinal: 1, Keep: 1}, {Period: "yearly", Ordinal: 1, Keep: 1}}
	selected, err := RetentionSelections([]RestorePoint{point}, rules, "Asia/Shanghai", now)
	if err != nil || len(selected[point.ID]) != 4 {
		t.Fatalf("local midnight: %v %v", selected, err)
	}
	selected, _ = RetentionSelections([]RestorePoint{point}, rules, "UTC", now)
	if len(selected[point.ID]) != 1 {
		t.Fatalf("only same ISO week survives UTC year boundary: %v", selected)
	}
}

func TestRetentionValidation(t *testing.T) {
	for _, rules := range [][]RetentionRule{{{Period: "bad", Ordinal: 1, Keep: 3}}, {{Period: "daily", Ordinal: 0, Keep: 3}}, {{Period: "daily", Ordinal: 1, Keep: 0}}, {{Period: "daily", Ordinal: 1, Keep: 3}, {Period: "daily", Ordinal: 1, Keep: 3}}} {
		if ValidateRetentionRules(rules, "UTC") == nil {
			t.Fatalf("accepted invalid rules %v", rules)
		}
	}
	if ValidateRetentionRules(nil, "bad-zone") == nil {
		t.Fatal("invalid timezone accepted")
	}
}
