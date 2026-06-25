package uptime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMiddlewarePassThrough(t *testing.T) {
	u := &Uptime{}
	handler := u.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHandlerRoutes(t *testing.T) {
	store := &memoryStore{}
	up, err := New(Config{
		ServiceID:      "api",
		ServiceName:    "API",
		SampleInterval: time.Second,
		Store:          store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()

	handler := up.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uptime", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "API") {
		t.Fatalf("dashboard body missing service: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="uptime-hovercard"`) ||
		!strings.Contains(rec.Body.String(), `.hovercard`) {
		t.Fatalf("dashboard missing global hovercard: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="theme-toggle"`) ||
		!strings.Contains(rec.Body.String(), `id="lang-toggle"`) {
		t.Fatalf("dashboard missing theme/language controls: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `class="header-divider"`) {
		t.Fatalf("dashboard missing monitor-style divider: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `window.uptimeInitialStatus`) ||
		!strings.Contains(rec.Body.String(), `window.uptimeConfig`) {
		t.Fatalf("dashboard missing embedded config/status")
	}
	if strings.Contains(rec.Body.String(), `title="Date:`) {
		t.Fatalf("dashboard still uses native title tooltip")
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uptime/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("json status = %d", rec.Code)
	}
	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Services) != 1 || status.Services[0].ID != "api" {
		t.Fatalf("unexpected services: %+v", status.Services)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/uptime", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uptime/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/uptime/api/status", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("head status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlerStorageErrorStatus(t *testing.T) {
	store := &memoryStore{}
	up, err := New(Config{
		ServiceID:      "api",
		SampleInterval: time.Second,
		Store:          store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	up.setLastError(errors.New("write failed"))

	rec := httptest.NewRecorder()
	up.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uptime/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Storage.Status != "degraded" || !strings.Contains(status.Storage.LastError, "write failed") {
		t.Fatalf("storage status = %+v", status.Storage)
	}
}

func TestHandlerQueryError(t *testing.T) {
	store := &memoryStore{}
	up, err := New(Config{
		ServiceID:      "api",
		SampleInterval: time.Second,
		Store:          store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	store.setQueryErr(errors.New("query failed"))

	rec := httptest.NewRecorder()
	up.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uptime", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBuildStatusUsesServiceSampleInterval(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 25, 0, 1, 0, 0, time.UTC)
	createdAt := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	store.services["fast"] = Service{
		ID:             "fast",
		Name:           "Fast",
		CreatedAt:      createdAt,
		LastSeenAt:     now.Add(-5 * time.Second),
		SampleInterval: 2 * time.Second,
	}
	store.services["slow"] = Service{
		ID:             "slow",
		Name:           "Slow",
		CreatedAt:      createdAt,
		LastSeenAt:     now.Add(-15 * time.Second),
		SampleInterval: 10 * time.Second,
	}
	store.samples = map[string]map[string]map[int64]struct{}{
		"fast": {
			"2026-06-25": {
				0: struct{}{},
				1: struct{}{},
			},
		},
		"slow": {
			"2026-06-25": {
				0: struct{}{},
				1: struct{}{},
				2: struct{}{},
			},
		},
	}

	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: 3 * time.Second,
		RetentionDays:  2,
		DaysToShow:     2,
		Timezone:       time.UTC,
		Store:          store,
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	status, err := up.buildStatus(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	services := make(map[string]ServiceStatus)
	for _, service := range status.Services {
		services[service.ID] = service
	}

	fast := services["fast"]
	if fast.SampleIntervalSeconds != 2 || fast.CurrentStatus != "down" {
		t.Fatalf("fast interval/status = %ds %s", fast.SampleIntervalSeconds, fast.CurrentStatus)
	}
	if fast.Daily[0].ExpectedSlots != 43200 {
		t.Fatalf("fast missing day expected slots = %d", fast.Daily[0].ExpectedSlots)
	}
	if fast.Daily[1].ExpectedSlots != 31 || fast.Daily[1].EstimatedDowntimeSeconds != 58 {
		t.Fatalf("fast today = %+v", fast.Daily[1])
	}

	slow := services["slow"]
	if slow.SampleIntervalSeconds != 10 || slow.CurrentStatus != "up" {
		t.Fatalf("slow interval/status = %ds %s", slow.SampleIntervalSeconds, slow.CurrentStatus)
	}
	if slow.Daily[0].ExpectedSlots != 8640 {
		t.Fatalf("slow missing day expected slots = %d", slow.Daily[0].ExpectedSlots)
	}
	if slow.Daily[1].ExpectedSlots != 7 || slow.Daily[1].EstimatedDowntimeSeconds != 40 {
		t.Fatalf("slow today = %+v", slow.Daily[1])
	}
}

func TestCachedSnapshotUsesTTL(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.setService(Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})

	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Timezone:       time.UTC,
		Store:          store,
		Snapshot: SnapshotConfig{
			CacheTTL: time.Minute,
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	first, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	listCalls, dailyCalls, todayCalls := store.queryCounts()

	second, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotList, gotDaily, gotToday := store.queryCounts(); gotList != listCalls || gotDaily != dailyCalls || gotToday != todayCalls {
		t.Fatalf("cache miss inside ttl: before=%d/%d/%d after=%d/%d/%d", listCalls, dailyCalls, todayCalls, gotList, gotDaily, gotToday)
	}
	if second.GeneratedAt != first.GeneratedAt {
		t.Fatalf("cached generated_at changed: first=%s second=%s", first.GeneratedAt, second.GeneratedAt)
	}

	second.Services[0].Name = "mutated"
	third, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.Services[0].Name != "API" {
		t.Fatalf("snapshot cache was mutated by caller: %+v", third.Services[0])
	}
}

func TestSnapshotBypassesCache(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.setService(Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})

	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Timezone:       time.UTC,
		Store:          store,
		Snapshot: SnapshotConfig{
			CacheTTL: time.Minute,
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	cached, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cached.Services[0].Name != "API" {
		t.Fatalf("cached name = %q", cached.Services[0].Name)
	}

	store.setService(Service{
		ID:             "api",
		Name:           "API v2",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})

	fresh, err := up.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Services[0].Name != "API v2" {
		t.Fatalf("fresh snapshot did not bypass cache: %+v", fresh.Services[0])
	}

	stillCached, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stillCached.Services[0].Name != "API" {
		t.Fatalf("cached snapshot unexpectedly refreshed: %+v", stillCached.Services[0])
	}
}

func TestCachedSnapshotReturnsStaleOnRefreshError(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.setService(Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})

	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Timezone:       time.UTC,
		Store:          store,
		Snapshot: SnapshotConfig{
			CacheTTL: time.Second,
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	seed, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	up.snapshotMu.Lock()
	up.snapshotCachedAt = time.Now().Add(-2 * time.Second)
	up.snapshotMu.Unlock()
	store.setQueryErr(errors.New("database offline"))

	stale, err := up.CachedSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stale.GeneratedAt != seed.GeneratedAt {
		t.Fatalf("expected stale generated_at: seed=%s stale=%s", seed.GeneratedAt, stale.GeneratedAt)
	}
	if stale.Storage.Status != "degraded" || !strings.Contains(stale.Storage.LastError, "database offline") {
		t.Fatalf("storage status = %+v", stale.Storage)
	}
}

func TestCachedSnapshotCanDisableStaleOnError(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.setService(Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	})

	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Timezone:       time.UTC,
		Store:          store,
		Snapshot: SnapshotConfig{
			CacheTTL:            time.Second,
			DisableStaleIfError: true,
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	if _, err := up.CachedSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	up.snapshotMu.Lock()
	up.snapshotCachedAt = time.Now().Add(-2 * time.Second)
	up.snapshotMu.Unlock()
	store.setQueryErr(errors.New("database offline"))

	if _, err := up.CachedSnapshot(context.Background()); err == nil {
		t.Fatal("expected refresh error")
	}
}

func TestAlertHookDeduplicatesTransitions(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.services["api"] = Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		SampleInterval: time.Second,
	}

	var events []AlertEvent
	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Store:          store,
		Alert: AlertConfig{
			Hook: func(_ context.Context, event AlertEvent) error {
				events = append(events, event)
				return nil
			},
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}

	up.evaluateAlerts(context.Background(), now)
	if len(events) != 0 {
		t.Fatalf("initial up emitted events: %+v", events)
	}

	store.services["api"] = Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now.Add(-3 * time.Second),
		SampleInterval: time.Second,
	}
	up.evaluateAlerts(context.Background(), now.Add(time.Second))
	up.evaluateAlerts(context.Background(), now.Add(2*time.Second))
	if len(events) != 1 {
		t.Fatalf("expected one down event, got %+v", events)
	}
	if events[0].PreviousStatus != AlertStatusUp || events[0].CurrentStatus != AlertStatusDown {
		t.Fatalf("bad down event: %+v", events[0])
	}

	store.services["api"] = Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now.Add(3 * time.Second),
		SampleInterval: time.Second,
	}
	up.evaluateAlerts(context.Background(), now.Add(3*time.Second))
	if len(events) != 2 {
		t.Fatalf("expected resolved event, got %+v", events)
	}
	if events[1].PreviousStatus != AlertStatusDown || events[1].CurrentStatus != AlertStatusUp {
		t.Fatalf("bad resolve event: %+v", events[1])
	}
}

func TestAlertHookNotifyOnFirstDown(t *testing.T) {
	store := &memoryStore{}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	store.services["api"] = Service{
		ID:             "api",
		Name:           "API",
		CreatedAt:      now.Add(-time.Hour),
		LastSeenAt:     now.Add(-10 * time.Second),
		SampleInterval: time.Second,
	}

	events := 0
	cfg, err := (Config{
		ServiceID:      "viewer",
		SampleInterval: time.Second,
		Store:          store,
		Alert: AlertConfig{
			NotifyOnFirstDown: true,
			Hook: func(context.Context, AlertEvent) error {
				events++
				return nil
			},
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	up := &Uptime{config: cfg, store: store}
	up.evaluateAlerts(context.Background(), now)
	if events != 1 {
		t.Fatalf("events = %d", events)
	}
}

type memoryStore struct {
	mu                sync.Mutex
	services          map[string]Service
	samples           map[string]map[string]map[int64]struct{}
	alertStates       map[string]AlertState
	queryErr          error
	listServicesCalls int
	queryDailyCalls   int
	todaySamplesCalls int
}

func (s *memoryStore) Init(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.services == nil {
		s.services = make(map[string]Service)
	}
	if s.samples == nil {
		s.samples = make(map[string]map[string]map[int64]struct{})
	}
	if s.alertStates == nil {
		s.alertStates = make(map[string]AlertState)
	}
	return nil
}

func (s *memoryStore) setService(service Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.services == nil {
		s.services = make(map[string]Service)
	}
	s.services[service.ID] = service
}

func (s *memoryStore) setQueryErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queryErr = err
}

func (s *memoryStore) queryCounts() (listServices, daily, today int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listServicesCalls, s.queryDailyCalls, s.todaySamplesCalls
}

func (s *memoryStore) UpsertService(_ context.Context, service Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.services[service.ID]; ok {
		service.CreatedAt = existing.CreatedAt
	}
	s.services[service.ID] = service
	return nil
}

func (s *memoryStore) UpsertInstance(context.Context, Instance) error {
	return nil
}

func (s *memoryStore) WriteHeartbeat(_ context.Context, heartbeat Heartbeat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	service := s.services[heartbeat.ServiceID]
	service.LastSeenAt = heartbeat.SeenAt
	s.services[heartbeat.ServiceID] = service
	byDay := s.samples[heartbeat.ServiceID]
	if byDay == nil {
		byDay = make(map[string]map[int64]struct{})
		s.samples[heartbeat.ServiceID] = byDay
	}
	slots := byDay[heartbeat.Day]
	if slots == nil {
		slots = make(map[int64]struct{})
		byDay[heartbeat.Day] = slots
	}
	slots[heartbeat.Slot] = struct{}{}
	return nil
}

func (s *memoryStore) RollupDaily(context.Context, RollupOptions) error {
	return nil
}

func (s *memoryStore) Cleanup(context.Context, CleanupOptions) error {
	return nil
}

func (s *memoryStore) ListServices(context.Context) ([]Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listServicesCalls++
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	services := make([]Service, 0, len(s.services))
	for _, service := range s.services {
		services = append(services, service)
	}
	return services, nil
}

func (s *memoryStore) QueryDaily(context.Context, QueryDailyOptions) ([]DailyStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queryDailyCalls++
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return nil, nil
}

func (s *memoryStore) QueryTodaySamples(_ context.Context, options QueryTodaySamplesOptions) ([]TodaySampleStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todaySamplesCalls++
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	var statuses []TodaySampleStatus
	for serviceID, byDay := range s.samples {
		slots := byDay[options.Day]
		if len(slots) == 0 {
			continue
		}
		statuses = append(statuses, TodaySampleStatus{
			ServiceID: serviceID,
			Day:       options.Day,
			UpSlots:   len(slots),
		})
	}
	return statuses, nil
}

func (s *memoryStore) ClaimAlertEvent(_ context.Context, state AlertState) (AlertDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.alertStates[state.ServiceID]
	if !ok {
		s.alertStates[state.ServiceID] = state
		return AlertDecision{
			Notify: state.NotifyOnFirstDown && state.Status == AlertStatusDown,
		}, nil
	}
	if previous.Status == state.Status {
		s.alertStates[state.ServiceID] = state
		return AlertDecision{}, nil
	}
	s.alertStates[state.ServiceID] = state
	return AlertDecision{
		Notify:         true,
		PreviousStatus: previous.Status,
	}, nil
}

func (s *memoryStore) Close() error {
	return nil
}
