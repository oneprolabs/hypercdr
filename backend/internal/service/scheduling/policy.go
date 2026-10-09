// Package scheduling evaluates persisted policies independently of HTTP.
package scheduling

import (
	"hypercdr-platform/platform/backend/internal/store"
	"time"
)

func NextPolicyFireAt(policy store.Policy, after time.Time) time.Time {
	return NextPolicyFireAtInLocation(policy, after, time.UTC)
}

func NextPolicyFireAtInLocation(policy store.Policy, after time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	after = after.UTC()
	switch policy.ScheduleType {
	case "interval":
		value := policy.IntervalValue
		if value <= 0 {
			value = 1
		}
		switch policy.IntervalUnit {
		case "minute", "minutes":
			return after.Add(time.Duration(value) * time.Minute).Truncate(time.Second)
		case "hour", "hours", "":
			return after.Add(time.Duration(value) * time.Hour).Truncate(time.Second)
		default:
			return after.Add(time.Hour).Truncate(time.Second)
		}
	case "daily":
		localAfter := after.In(location)
		next := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, location)
		if !next.After(localAfter) {
			next = next.AddDate(0, 0, 1)
		}
		return next.UTC()
	case "weekly":
		localAfter := after.In(location)
		target := time.Weekday(clampWeekday(policy.WeekDay))
		next := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, location)
		days := (int(target) - int(localAfter.Weekday()) + 7) % 7
		next = next.AddDate(0, 0, days)
		if !next.After(localAfter) {
			next = next.AddDate(0, 0, 7)
		}
		return next.UTC()
	case "monthly":
		localAfter := after.In(location)
		day := clampMonthDay(policy.MonthDay)
		next := monthlyTime(localAfter.Year(), localAfter.Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute), location)
		if !next.After(localAfter) {
			nextMonth := localAfter.AddDate(0, 1, 0)
			next = monthlyTime(nextMonth.Year(), nextMonth.Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute), location)
		}
		return next.UTC()
	default:
		return after.Add(time.Hour).Truncate(time.Second)
	}
}

func monthlyTime(year int, month time.Month, day int, hour int, minute int, location *time.Location) time.Time {
	last := time.Date(year, month+1, 0, hour, minute, 0, 0, location).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}

func ScheduleMatchesPolicy(next time.Time, policy store.Policy, location *time.Location) bool {
	if next.IsZero() {
		return false
	}
	if policy.ScheduleType == "interval" {
		return true
	}
	if location == nil {
		location = time.UTC
	}
	local := next.In(location)
	if local.Hour() != clampHour(policy.Hour) || local.Minute() != clampMinute(policy.Minute) {
		return false
	}
	switch policy.ScheduleType {
	case "daily":
		return true
	case "weekly":
		return local.Weekday() == time.Weekday(clampWeekday(policy.WeekDay))
	case "monthly":
		return local.Day() == monthlyTime(local.Year(), local.Month(), clampMonthDay(policy.MonthDay), local.Hour(), local.Minute(), location).Day()
	default:
		return false
	}
}

func clampMinute(value int) int {
	if value < 0 || value > 59 {
		return 0
	}
	return value
}

func clampHour(value int) int {
	if value < 0 || value > 23 {
		return 0
	}
	return value
}

func clampWeekday(value int) int {
	if value < 0 || value > 6 {
		return 0
	}
	return value
}

func clampMonthDay(value int) int {
	if value < 1 || value > 31 {
		return 1
	}
	return value
}
