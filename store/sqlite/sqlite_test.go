package sqlite

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gofurry/uptime"
)

func TestHeartbeatDedupAndServiceAggregation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	upsertTestService(t, store, "api", now)
	upsertTestInstance(t, store, 1, "api", now)
	upsertTestInstance(t, store, 2, "api", now)

	writeTestHeartbeat(t, store, "api", 1, "2026-06-25", 10, now)
	writeTestHeartbeat(t, store, "api", 1, "2026-06-25", 10, now.Add(time.Second))
	writeTestHeartbeat(t, store, "api", 2, "2026-06-25", 10, now.Add(2*time.Second))

	rows, err := store.QueryTodaySamples(ctx, uptime.QueryTodaySamplesOptions{Day: "2026-06-25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UpSlots != 1 {
		t.Fatalf("expected one distinct service slot, got %+v", rows)
	}

	writeTestHeartbeat(t, store, "api", 2, "2026-06-25", 11, now.Add(3*time.Second))
	rows, err = store.QueryTodaySamples(ctx, uptime.QueryTodaySamplesOptions{Day: "2026-06-25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UpSlots != 2 {
		t.Fatalf("expected two distinct service slots, got %+v", rows)
	}
}

func TestConcurrentHeartbeatWrites(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	upsertTestService(t, store, "api", now)
	upsertTestInstance(t, store, 1, "api", now)

	const writers = 32
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(slot int64) {
			defer wg.Done()
			errs <- store.WriteHeartbeat(context.Background(), uptime.Heartbeat{
				ServiceID:  "api",
				InstanceID: 1,
				Day:        "2026-06-25",
				Slot:       slot,
				SeenAt:     now.Add(time.Duration(slot) * time.Second),
			})
		}(int64(i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.QueryTodaySamples(context.Background(), uptime.QueryTodaySamplesOptions{Day: "2026-06-25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UpSlots != writers {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRollupCleanupAndReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/uptime.db"
	store := New(Config{Path: path})
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	upsertTestService(t, store, "api", now)
	upsertTestInstance(t, store, 1, "api", now)
	writeTestHeartbeat(t, store, "api", 1, "2026-06-23", 1, now)
	writeTestHeartbeat(t, store, "api", 1, "2026-06-24", 1, now)
	writeTestHeartbeat(t, store, "api", 1, "2026-06-25", 1, now)

	err := store.RollupDaily(ctx, uptime.RollupOptions{
		BeforeDay: "2026-06-25",
		ExpectedSlotsForDay: func(string) int {
			return 10
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	daily, err := store.QueryDaily(ctx, uptime.QueryDailyOptions{FromDay: "2026-06-23", ToDay: "2026-06-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 2 {
		t.Fatalf("daily rows = %+v", daily)
	}
	for _, row := range daily {
		if row.UpSlots != 1 || row.ExpectedSlots != 10 || row.UptimeRate != 0.1 || !row.Finalized {
			t.Fatalf("bad daily row: %+v", row)
		}
	}

	if err := store.Cleanup(ctx, uptime.CleanupOptions{
		DailyBeforeDay:   "2026-05-26",
		SamplesBeforeDay: "2026-06-24",
	}); err != nil {
		t.Fatal(err)
	}

	oldRows, err := store.QueryTodaySamples(ctx, uptime.QueryTodaySamplesOptions{Day: "2026-06-23"})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldRows) != 0 {
		t.Fatalf("old samples still present: %+v", oldRows)
	}
	yesterdayRows, err := store.QueryTodaySamples(ctx, uptime.QueryTodaySamplesOptions{Day: "2026-06-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(yesterdayRows) != 1 {
		t.Fatalf("yesterday samples not retained: %+v", yesterdayRows)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := New(Config{Path: path})
	if err := reopened.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	services, err := reopened.ListServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 || services[0].ID != "api" {
		t.Fatalf("services after reopen: %+v", services)
	}
}

func TestRollupUsesServiceExpectedSlots(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	upsertTestService(t, store, "api", now)
	upsertTestService(t, store, "worker", now)
	upsertTestInstance(t, store, 1, "api", now)
	upsertTestInstance(t, store, 2, "worker", now)
	writeTestHeartbeat(t, store, "api", 1, "2026-06-24", 1, now)
	writeTestHeartbeat(t, store, "worker", 2, "2026-06-24", 1, now)

	err := store.RollupDaily(ctx, uptime.RollupOptions{
		BeforeDay: "2026-06-25",
		ExpectedSlotsForServiceDay: func(serviceID, day string) int {
			if serviceID == "worker" {
				return 20
			}
			return 10
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	daily, err := store.QueryDaily(ctx, uptime.QueryDailyOptions{FromDay: "2026-06-24", ToDay: "2026-06-24"})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{"api": 10, "worker": 20}
	for _, row := range daily {
		if row.ExpectedSlots != expected[row.ServiceID] {
			t.Fatalf("expected slots for %s = %d", row.ServiceID, row.ExpectedSlots)
		}
	}
}

func TestClaimAlertEvent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	decision, err := store.ClaimAlertEvent(ctx, uptime.AlertState{
		ServiceID:  "api",
		Status:     uptime.AlertStatusUp,
		LastSeenAt: now,
		CheckedAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Notify {
		t.Fatalf("initial up should not notify: %+v", decision)
	}

	decision, err = store.ClaimAlertEvent(ctx, uptime.AlertState{
		ServiceID:  "api",
		Status:     uptime.AlertStatusDown,
		LastSeenAt: now.Add(-time.Minute),
		CheckedAt:  now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Notify || decision.PreviousStatus != uptime.AlertStatusUp {
		t.Fatalf("down decision = %+v", decision)
	}

	decision, err = store.ClaimAlertEvent(ctx, uptime.AlertState{
		ServiceID:  "api",
		Status:     uptime.AlertStatusDown,
		LastSeenAt: now.Add(-time.Minute),
		CheckedAt:  now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Notify {
		t.Fatalf("duplicate down should not notify: %+v", decision)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store := New(Config{Path: t.TempDir() + "/uptime.db"})
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func upsertTestService(t *testing.T, store *Store, id string, now time.Time) {
	t.Helper()
	err := store.UpsertService(context.Background(), uptime.Service{
		ID:             id,
		Name:           id,
		CreatedAt:      now,
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func upsertTestInstance(t *testing.T, store *Store, id int64, serviceID string, now time.Time) {
	t.Helper()
	err := store.UpsertInstance(context.Background(), uptime.Instance{
		ID:         id,
		ServiceID:  serviceID,
		StartedAt:  now,
		LastSeenAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestHeartbeat(t *testing.T, store *Store, serviceID string, instanceID int64, day string, slot int64, seenAt time.Time) {
	t.Helper()
	err := store.WriteHeartbeat(context.Background(), uptime.Heartbeat{
		ServiceID:  serviceID,
		InstanceID: instanceID,
		Day:        day,
		Slot:       slot,
		SeenAt:     seenAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}
