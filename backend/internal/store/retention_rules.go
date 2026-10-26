package store

import (
	"fmt"
	"sort"
	"time"
)

type RetentionRule struct {
	Period  string `json:"period"`
	Ordinal int    `json:"ordinal"`
	Keep    int    `json:"keep"`
}

type RetentionReason struct {
	Period  string    `json:"period"`
	Ordinal int       `json:"ordinal"`
	Until   time.Time `json:"until"`
}

func ValidateRetentionRules(rules []RetentionRule, zone string) error {
	if len(rules) > 32 {
		return fmt.Errorf("at most 32 retention rules are allowed")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("invalid retention timezone")
	}
	seen := map[RetentionRule]bool{}
	for _, rule := range rules {
		switch rule.Period {
		case "daily", "weekly", "monthly", "yearly":
		default:
			return fmt.Errorf("invalid retention period")
		}
		if rule.Ordinal < 1 || rule.Ordinal > 100000 || rule.Keep < 1 || rule.Keep > 1000 {
			return fmt.Errorf("retention ordinal and duration must be positive and within limits")
		}
		if seen[rule] {
			return fmt.Errorf("duplicate retention rule")
		}
		seen[rule] = true
	}
	return nil
}

func retentionPeriodStart(value time.Time, period string, location *time.Location) time.Time {
	value = value.In(location)
	year, month, day := value.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	switch period {
	case "weekly":
		return start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
	case "monthly":
		return time.Date(year, month, 1, 0, 0, 0, 0, location)
	case "yearly":
		return time.Date(year, 1, 1, 0, 0, 0, 0, location)
	}
	return start
}

func retentionPeriodAdd(value time.Time, period string, count int) time.Time {
	switch period {
	case "weekly":
		return value.AddDate(0, 0, count*7)
	case "monthly":
		return value.AddDate(0, count, 0)
	case "yearly":
		return value.AddDate(count, 0, 0)
	default:
		return value.AddDate(0, 0, count)
	}
}

func restorePointOrderTime(point RestorePoint) time.Time {
	if !point.TaskCreatedAt.IsZero() {
		return point.TaskCreatedAt
	}
	if !point.StartedAt.IsZero() {
		return point.StartedAt
	}
	return point.CreatedAt
}

func RetentionSelections(points []RestorePoint, rules []RetentionRule, zone string, now time.Time) (map[string][]RetentionReason, error) {
	if err := ValidateRetentionRules(rules, zone); err != nil {
		return nil, err
	}
	location, _ := time.LoadLocation(zone)
	ordered := append([]RestorePoint(nil), points...)
	sort.Slice(ordered, func(left, right int) bool {
		leftTime, rightTime := restorePointOrderTime(ordered[left]), restorePointOrderTime(ordered[right])
		if leftTime.Equal(rightTime) {
			return ordered[left].ID < ordered[right].ID
		}
		return leftTime.Before(rightTime)
	})
	selected := map[string][]RetentionReason{}
	for _, rule := range rules {
		cutoff := retentionPeriodAdd(retentionPeriodStart(now, rule.Period, location), rule.Period, 1-rule.Keep)
		counts := map[time.Time]int{}
		for _, point := range ordered {
			if point.Status != "available" && point.Status != "deleted" {
				continue
			}
			timestamp := restorePointOrderTime(point)
			if timestamp.After(now) {
				continue
			}
			start := retentionPeriodStart(timestamp, rule.Period, location)
			if start.Before(cutoff) {
				continue
			}
			counts[start]++
			if counts[start] == rule.Ordinal && point.Status == "available" {
				selected[point.ID] = append(selected[point.ID], RetentionReason{Period: rule.Period, Ordinal: rule.Ordinal, Until: retentionPeriodAdd(start, rule.Period, rule.Keep).UTC()})
			}
		}
	}
	return selected, nil
}
