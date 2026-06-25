package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/internal/timeutil"
	"github.com/gofurry/uptime/store/sqlite"
)

func TestProbeWritesHeartbeatOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := sqlite.New(sqlite.Config{Path: t.TempDir() + "/uptime.db"})
	probe, err := New(Config{
		ServiceID:      "homepage",
		URL:            server.URL,
		ExpectedStatus: []int{http.StatusNoContent},
		Interval:       time.Hour,
		Timeout:        time.Second,
		Store:          store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	today := timeutil.DayOf(time.Now(), time.Local)
	rows, err := store.QueryTodaySamples(context.Background(), uptime.QueryTodaySamplesOptions{Day: today})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ServiceID != "homepage" || rows[0].UpSlots != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestProbeDoesNotWriteHeartbeatOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := sqlite.New(sqlite.Config{Path: t.TempDir() + "/uptime.db"})
	probe, err := New(Config{
		ServiceID: "homepage",
		URL:       server.URL,
		Interval:  time.Hour,
		Timeout:   time.Second,
		Store:     store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	today := timeutil.DayOf(time.Now(), time.Local)
	rows, err := store.QueryTodaySamples(context.Background(), uptime.QueryTodaySamplesOptions{Day: today})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestNormalizeConfig(t *testing.T) {
	if _, err := normalizeConfig(Config{}); err == nil {
		t.Fatal("expected missing service id error")
	}
	if _, err := normalizeConfig(Config{ServiceID: "home"}); err == nil {
		t.Fatal("expected missing url error")
	}
	if _, err := normalizeConfig(Config{ServiceID: "home", URL: "http://example.com"}); err == nil {
		t.Fatal("expected missing store error")
	}
}
