package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofurry/uptime"
	instanceid "github.com/gofurry/uptime/internal/id"
	"github.com/gofurry/uptime/internal/timeutil"
)

const (
	defaultInterval = 30 * time.Second
	defaultTimeout  = 5 * time.Second
)

type Config struct {
	ServiceID          string
	ServiceName        string
	ServiceDescription string

	URL    string
	Method string

	ExpectedStatus []int
	Interval       time.Duration
	Timeout        time.Duration
	Timezone       *time.Location

	NodeID      int64
	InstanceID  int64
	IDGenerator uptime.IDGenerator

	Client *http.Client
	Store  uptime.Store

	Logger uptime.Logger
}

type Probe struct {
	config   Config
	store    uptime.Store
	client   *http.Client
	service  uptime.Service
	instance uptime.Instance

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type Result struct {
	URL        string
	Method     string
	StatusCode int
	Up         bool
	CheckedAt  time.Time
	Duration   time.Duration
}

func New(config Config) (*Probe, error) {
	cfg, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if err := cfg.Store.Init(ctx); err != nil {
		return nil, fmt.Errorf("uptime probe: init store: %w", err)
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
	service := uptime.Service{
		ID:             cfg.ServiceID,
		Name:           cfg.ServiceName,
		Description:    cfg.ServiceDescription,
		CreatedAt:      now,
		SampleInterval: cfg.Interval,
	}
	instance := uptime.Instance{
		ID:        instanceID,
		ServiceID: cfg.ServiceID,
		Hostname:  hostname,
		PID:       os.Getpid(),
		StartedAt: now,
	}
	if err := cfg.Store.UpsertService(ctx, service); err != nil {
		_ = cfg.Store.Close()
		return nil, fmt.Errorf("uptime probe: upsert service: %w", err)
	}
	if err := cfg.Store.UpsertInstance(ctx, instance); err != nil {
		_ = cfg.Store.Close()
		return nil, fmt.Errorf("uptime probe: upsert instance: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	p := &Probe{
		config:   cfg,
		store:    cfg.Store,
		client:   cfg.Client,
		service:  service,
		instance: instance,
		ctx:      runCtx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	if p.client == nil {
		p.client = &http.Client{Timeout: cfg.Timeout}
	}
	if _, err := p.Check(ctx); err != nil {
		cfg.Logger.Printf("uptime probe: initial check %s: %v", cfg.URL, err)
	}
	go p.loop()
	return p, nil
}

func (p *Probe) Close() error {
	if p == nil {
		return nil
	}
	p.cancel()
	<-p.done
	return p.store.Close()
}

func (p *Probe) Check(ctx context.Context) (Result, error) {
	started := time.Now()
	result := Result{
		URL:       p.config.URL,
		Method:    p.config.Method,
		CheckedAt: started.UTC(),
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, p.config.Method, p.config.URL, nil)
	if err != nil {
		return result, err
	}
	resp, err := p.client.Do(req)
	result.Duration = time.Since(started)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	result.StatusCode = resp.StatusCode
	if !p.statusOK(resp.StatusCode) {
		return result, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	result.Up = true
	if err := p.store.WriteHeartbeat(ctx, uptime.Heartbeat{
		ServiceID:  p.config.ServiceID,
		InstanceID: p.instance.ID,
		Day:        timeutil.DayOf(started, p.config.Timezone),
		Slot:       timeutil.SlotOf(started, p.config.Interval, p.config.Timezone),
		SeenAt:     started,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (p *Probe) loop() {
	defer close(p.done)

	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if _, err := p.Check(p.ctx); err != nil {
				p.config.Logger.Printf("uptime probe: check %s: %v", p.config.URL, err)
			}
		}
	}
}

func (p *Probe) statusOK(status int) bool {
	if len(p.config.ExpectedStatus) == 0 {
		return status >= 200 && status < 400
	}
	for _, expected := range p.config.ExpectedStatus {
		if status == expected {
			return true
		}
	}
	return false
}

func normalizeConfig(config Config) (Config, error) {
	if config.ServiceID == "" {
		return Config{}, errors.New("uptime probe: service id is required")
	}
	if config.ServiceName == "" {
		config.ServiceName = config.ServiceID
	}
	if config.URL == "" {
		return Config{}, errors.New("uptime probe: url is required")
	}
	if config.Method == "" {
		config.Method = http.MethodGet
	}
	if config.Interval == 0 {
		config.Interval = defaultInterval
	}
	if config.Interval < time.Second {
		return Config{}, errors.New("uptime probe: interval must be at least 1s")
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.Timeout < time.Second {
		return Config{}, errors.New("uptime probe: timeout must be at least 1s")
	}
	if config.Timezone == nil {
		config.Timezone = time.Local
	}
	if config.Store == nil {
		return Config{}, errors.New("uptime probe: store is required")
	}
	if config.Logger == nil {
		config.Logger = log.Default()
	}
	return config, nil
}
