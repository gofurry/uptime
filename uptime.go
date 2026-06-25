package uptime

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	instanceid "github.com/gofurry/uptime/internal/id"
	"github.com/gofurry/uptime/internal/timeutil"
)

const maintenanceInterval = time.Minute

// Uptime records service heartbeats and serves uptime history.
type Uptime struct {
	config Config
	store  Store

	service  Service
	instance Instance

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	closeOnce sync.Once

	errMu     sync.RWMutex
	lastErr   error
	lastErrAt time.Time

	maintenanceMu      sync.Mutex
	lastMaintenance    time.Time
	lastMaintenanceDay string

	alertMu        sync.Mutex
	lastAlertCheck time.Time

	snapshotMu       sync.Mutex
	snapshotCache    Snapshot
	snapshotCachedAt time.Time
	snapshotHasCache bool
}

// New initializes the store, writes the first heartbeat, and starts recording.
func New(config Config) (*Uptime, error) {
	cfg, err := config.normalized()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if err := cfg.Store.Init(ctx); err != nil {
		return nil, fmt.Errorf("uptime: init store: %w", err)
	}

	now := time.Now()
	instanceID := cfg.InstanceID
	if instanceID == 0 && cfg.IDGenerator != nil {
		instanceID = cfg.IDGenerator.NextID()
	}
	if instanceID == 0 {
		instanceID = instanceid.InstanceID(cfg.NodeID)
	}

	hostname, _ := os.Hostname()
	service := Service{
		ID:             cfg.ServiceID,
		Name:           cfg.ServiceName,
		Description:    cfg.ServiceDescription,
		CreatedAt:      now,
		LastSeenAt:     now,
		SampleInterval: cfg.SampleInterval,
	}
	instance := Instance{
		ID:         instanceID,
		ServiceID:  cfg.ServiceID,
		Hostname:   hostname,
		PID:        os.Getpid(),
		StartedAt:  now,
		LastSeenAt: now,
	}

	if err := cfg.Store.UpsertService(ctx, service); err != nil {
		_ = cfg.Store.Close()
		return nil, fmt.Errorf("uptime: upsert service: %w", err)
	}
	if err := cfg.Store.UpsertInstance(ctx, instance); err != nil {
		_ = cfg.Store.Close()
		return nil, fmt.Errorf("uptime: upsert instance: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	u := &Uptime{
		config:   cfg,
		store:    cfg.Store,
		service:  service,
		instance: instance,
		ctx:      runCtx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	if err := u.runMaintenance(ctx, now, true); err != nil {
		_ = cfg.Store.Close()
		return nil, fmt.Errorf("uptime: maintenance: %w", err)
	}
	_ = u.writeHeartbeat(ctx, now)

	go u.loop()
	return u, nil
}

// Close stops background work and closes the store.
func (u *Uptime) Close() error {
	if u == nil {
		return nil
	}
	var err error
	u.closeOnce.Do(func() {
		u.cancel()
		<-u.done
		err = u.store.Close()
	})
	return err
}

// LastError returns the most recent runtime store error, if any.
func (u *Uptime) LastError() (error, time.Time) {
	u.errMu.RLock()
	defer u.errMu.RUnlock()
	return u.lastErr, u.lastErrAt
}

func (u *Uptime) loop() {
	defer close(u.done)

	ticker := time.NewTicker(u.config.SampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-u.ctx.Done():
			return
		case now := <-ticker.C:
			_ = u.writeHeartbeat(u.ctx, now)
		}
	}
}

func (u *Uptime) writeHeartbeat(ctx context.Context, now time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	heartbeat := Heartbeat{
		ServiceID:  u.config.ServiceID,
		InstanceID: u.instance.ID,
		Day:        timeutil.DayOf(now, u.config.Timezone),
		Slot:       timeutil.SlotOf(now, u.config.SampleInterval, u.config.Timezone),
		SeenAt:     now,
	}
	if err := u.store.WriteHeartbeat(ctx, heartbeat); err != nil {
		u.setLastError(err)
		u.config.Logger.Printf("uptime: write heartbeat: %v", err)
		return err
	}
	if err := u.runMaintenance(ctx, now, false); err != nil {
		return err
	}
	u.evaluateAlerts(ctx, now)
	u.clearLastError()
	return nil
}

func (u *Uptime) runMaintenance(ctx context.Context, now time.Time, force bool) error {
	today := timeutil.DayOf(now, u.config.Timezone)

	u.maintenanceMu.Lock()
	defer u.maintenanceMu.Unlock()

	if !force && u.lastMaintenanceDay == today && now.Sub(u.lastMaintenance) < maintenanceInterval {
		return nil
	}

	expected := func(day string) int {
		return timeutil.ExpectedSlotsForDay(day, u.config.SampleInterval, u.config.Timezone)
	}
	serviceIntervals, err := u.serviceIntervals(ctx)
	if err != nil {
		u.setLastError(err)
		u.config.Logger.Printf("uptime: list services for maintenance: %v", err)
		return err
	}
	expectedForService := func(serviceID, day string) int {
		interval := serviceIntervals[serviceID]
		if interval < time.Second {
			interval = u.config.SampleInterval
		}
		return timeutil.ExpectedSlotsForDay(day, interval, u.config.Timezone)
	}
	if err := u.store.RollupDaily(ctx, RollupOptions{
		BeforeDay:                  today,
		ExpectedSlotsForDay:        expected,
		ExpectedSlotsForServiceDay: expectedForService,
	}); err != nil {
		u.setLastError(err)
		u.config.Logger.Printf("uptime: rollup daily: %v", err)
		return err
	}

	dailyBefore := timeutil.AddDays(today, -u.config.RetentionDays, u.config.Timezone)
	samplesBefore := timeutil.AddDays(today, -1, u.config.Timezone)
	if err := u.store.Cleanup(ctx, CleanupOptions{
		DailyBeforeDay:   dailyBefore,
		SamplesBeforeDay: samplesBefore,
	}); err != nil {
		u.setLastError(err)
		u.config.Logger.Printf("uptime: cleanup: %v", err)
		return err
	}

	u.lastMaintenance = now
	u.lastMaintenanceDay = today
	return nil
}

func (u *Uptime) serviceIntervals(ctx context.Context) (map[string]time.Duration, error) {
	services, err := u.store.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	intervals := make(map[string]time.Duration, len(services))
	for _, service := range services {
		intervals[service.ID] = u.serviceSampleInterval(service)
	}
	return intervals, nil
}

func (u *Uptime) evaluateAlerts(ctx context.Context, now time.Time) {
	if u.config.Alert.Hook == nil {
		return
	}

	u.alertMu.Lock()
	if !u.lastAlertCheck.IsZero() && now.Sub(u.lastAlertCheck) < u.config.Alert.CheckInterval {
		u.alertMu.Unlock()
		return
	}
	u.lastAlertCheck = now
	u.alertMu.Unlock()

	stateStore, ok := u.store.(AlertStateStore)
	if !ok {
		u.config.Logger.Printf("uptime: alert hook requires alert state support")
		return
	}

	services, err := u.store.ListServices(ctx)
	if err != nil {
		u.config.Logger.Printf("uptime: list services for alerts: %v", err)
		return
	}
	for _, service := range services {
		interval := u.serviceSampleInterval(service)
		status := currentStatus(now, service.LastSeenAt, interval)
		decision, err := stateStore.ClaimAlertEvent(ctx, AlertState{
			ServiceID:         service.ID,
			Status:            status,
			LastSeenAt:        service.LastSeenAt,
			CheckedAt:         now,
			NotifyOnFirstDown: u.config.Alert.NotifyOnFirstDown,
		})
		if err != nil {
			u.config.Logger.Printf("uptime: claim alert event for %s: %v", service.ID, err)
			continue
		}
		if !decision.Notify {
			continue
		}
		event := AlertEvent{
			ServiceID:      service.ID,
			ServiceName:    service.Name,
			Description:    service.Description,
			PreviousStatus: decision.PreviousStatus,
			CurrentStatus:  status,
			LastSeenAt:     service.LastSeenAt.UTC(),
			DetectedAt:     now.UTC(),
			SampleInterval: interval,
		}
		if status == AlertStatusDown && !service.LastSeenAt.IsZero() {
			event.DownFor = now.Sub(service.LastSeenAt)
		}
		if err := u.config.Alert.Hook(ctx, event); err != nil {
			u.config.Logger.Printf("uptime: alert hook for %s: %v", service.ID, err)
		}
	}
}

func (u *Uptime) setLastError(err error) {
	if err == nil {
		return
	}
	u.errMu.Lock()
	u.lastErr = err
	u.lastErrAt = time.Now()
	u.errMu.Unlock()
}

func (u *Uptime) clearLastError() {
	u.errMu.Lock()
	u.lastErr = nil
	u.lastErrAt = time.Time{}
	u.errMu.Unlock()
}
