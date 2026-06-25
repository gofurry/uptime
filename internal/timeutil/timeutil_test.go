package timeutil

import (
	"testing"
	"time"
)

func TestDaySlotAndExpectedSlots(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, loc)

	if got := DayOf(now.UTC(), loc); got != "2026-06-25" {
		t.Fatalf("day = %q", got)
	}
	if got := SlotOf(now, 3*time.Second, loc); got != 14400 {
		t.Fatalf("slot = %d", got)
	}
	if got := ExpectedSlotsSoFar(now, 3*time.Second, loc); got != 14401 {
		t.Fatalf("expected so far = %d", got)
	}
	if got := ExpectedSlotsForDay("2026-06-25", 3*time.Second, loc); got != 28800 {
		t.Fatalf("expected full day = %d", got)
	}
	if got := AddDays("2026-06-25", -1, loc); got != "2026-06-24" {
		t.Fatalf("add days = %q", got)
	}
}
