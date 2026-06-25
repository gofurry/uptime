package timeutil

import "time"

func DayOf(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

func ParseDay(day string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", day, loc)
}

func StartOfDay(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func SlotOf(t time.Time, interval time.Duration, loc *time.Location) int64 {
	if interval <= 0 {
		return 0
	}
	elapsed := t.In(loc).Sub(StartOfDay(t, loc))
	if elapsed < 0 {
		return 0
	}
	return int64(elapsed / interval)
}

func ExpectedSlotsSoFar(now time.Time, interval time.Duration, loc *time.Location) int {
	slot := SlotOf(now, interval, loc)
	if slot < 0 {
		return 1
	}
	return int(slot) + 1
}

func ExpectedSlotsForDay(day string, interval time.Duration, loc *time.Location) int {
	start, err := ParseDay(day, loc)
	if err != nil || interval <= 0 {
		return 0
	}
	end := start.AddDate(0, 0, 1)
	duration := end.Sub(start)
	return CeilDuration(duration, interval)
}

func CeilDuration(duration, interval time.Duration) int {
	if duration <= 0 || interval <= 0 {
		return 0
	}
	return int((duration + interval - 1) / interval)
}

func AddDays(day string, days int, loc *time.Location) string {
	start, err := ParseDay(day, loc)
	if err != nil {
		return day
	}
	return start.AddDate(0, 0, days).Format("2006-01-02")
}
