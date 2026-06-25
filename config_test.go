package uptime

import (
	"errors"
	"testing"
	"time"
)

func TestConfigValidationAndDefaults(t *testing.T) {
	if _, err := (Config{}).normalized(); !errors.Is(err, ErrMissingServiceID) {
		t.Fatalf("expected missing service id, got %v", err)
	}
	if _, err := (Config{ServiceID: "api"}).normalized(); !errors.Is(err, ErrMissingStore) {
		t.Fatalf("expected missing store, got %v", err)
	}

	cfg, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServiceName != "api" {
		t.Fatalf("service name = %q", cfg.ServiceName)
	}
	if cfg.SampleInterval != 3*time.Second {
		t.Fatalf("sample interval = %s", cfg.SampleInterval)
	}
	if cfg.RetentionDays != 90 || cfg.DaysToShow != 90 {
		t.Fatalf("unexpected days: retention=%d show=%d", cfg.RetentionDays, cfg.DaysToShow)
	}
	if cfg.Snapshot.CacheTTL != cfg.SampleInterval {
		t.Fatalf("snapshot cache ttl = %s", cfg.Snapshot.CacheTTL)
	}
	if cfg.Snapshot.DisableCache || cfg.Snapshot.DisableStaleIfError {
		t.Fatalf("unexpected snapshot flags: %+v", cfg.Snapshot)
	}
	if cfg.UI.Path != "/uptime" {
		t.Fatalf("ui path = %q", cfg.UI.Path)
	}
	if cfg.UI.DefaultTheme != ThemeDark {
		t.Fatalf("default theme = %q", cfg.UI.DefaultTheme)
	}
	if cfg.UI.DefaultLanguage != LanguageEnglish {
		t.Fatalf("default language = %q", cfg.UI.DefaultLanguage)
	}
	if cfg.UI.Title != defaultUITitle || cfg.UI.Description != defaultUIDescription || cfg.UI.Footer != defaultUIFooter {
		t.Fatalf("unexpected ui copy: %+v", cfg.UI)
	}
	if cfg.UI.Background != BackgroundSolid {
		t.Fatalf("background = %q", cfg.UI.Background)
	}
}

func TestUIConfigValidation(t *testing.T) {
	if _, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
		UI: UIConfig{
			DefaultTheme: "blue",
		},
	}).normalized(); err == nil {
		t.Fatal("expected invalid theme error")
	}

	if _, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
		UI: UIConfig{
			DefaultLanguage: "fr",
		},
	}).normalized(); err == nil {
		t.Fatal("expected invalid language error")
	}

	if _, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
		UI: UIConfig{
			Background: "noise",
		},
	}).normalized(); err == nil {
		t.Fatal("expected invalid background error")
	}
}

func TestSnapshotConfigValidation(t *testing.T) {
	base := DefaultConfig()
	base.ServiceID = "api"
	base.Store = &memoryStore{}
	base.SampleInterval = 10 * time.Second
	defaulted, err := base.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if defaulted.Snapshot.CacheTTL != 10*time.Second {
		t.Fatalf("snapshot cache ttl should follow sample interval, got %s", defaulted.Snapshot.CacheTTL)
	}

	if _, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
		Snapshot: SnapshotConfig{
			CacheTTL: time.Millisecond,
		},
	}).normalized(); err == nil {
		t.Fatal("expected invalid snapshot cache ttl error")
	}

	cfg, err := (Config{
		ServiceID: "api",
		Store:     &memoryStore{},
		Snapshot: SnapshotConfig{
			CacheTTL:     time.Millisecond,
			DisableCache: true,
		},
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Snapshot.DisableCache {
		t.Fatalf("snapshot config = %+v", cfg.Snapshot)
	}
}

func TestStatusHelpers(t *testing.T) {
	ui := DefaultConfig().UI
	if got := colorFor(0.995, true, ui); got != "green" {
		t.Fatalf("green threshold color = %q", got)
	}
	if got := colorFor(0.96, true, ui); got != "yellow" {
		t.Fatalf("yellow threshold color = %q", got)
	}
	if got := colorFor(0.8, true, ui); got != "red" {
		t.Fatalf("red threshold color = %q", got)
	}
	if got := colorFor(0, false, ui); got != "gray" {
		t.Fatalf("no data color = %q", got)
	}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	if got := currentStatus(now, now.Add(-5*time.Second), 3*time.Second); got != "up" {
		t.Fatalf("current status = %q", got)
	}
	if got := currentStatus(now, now.Add(-7*time.Second), 3*time.Second); got != "down" {
		t.Fatalf("current status = %q", got)
	}
}
