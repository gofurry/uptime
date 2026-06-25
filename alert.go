package uptime

import (
	"context"
	"time"
)

const (
	AlertStatusUp   = "up"
	AlertStatusDown = "down"
)

// AlertHook receives deduplicated service status transitions.
//
// Hooks are optional. They are called after the shared store has claimed the
// transition, so built-in stores avoid duplicate notifications across multiple
// processes using the same SQLite or PostgreSQL storage.
type AlertHook func(context.Context, AlertEvent) error

// AlertConfig controls optional status-transition notifications.
type AlertConfig struct {
	Hook AlertHook

	CheckInterval     time.Duration
	NotifyOnFirstDown bool
}

// AlertEvent describes one service status transition.
type AlertEvent struct {
	ServiceID      string        `json:"service_id"`
	ServiceName    string        `json:"service_name"`
	Description    string        `json:"description,omitempty"`
	PreviousStatus string        `json:"previous_status"`
	CurrentStatus  string        `json:"current_status"`
	LastSeenAt     time.Time     `json:"last_seen_at"`
	DetectedAt     time.Time     `json:"detected_at"`
	SampleInterval time.Duration `json:"sample_interval"`
	DownFor        time.Duration `json:"down_for"`
}

type AlertState struct {
	ServiceID         string
	Status            string
	LastSeenAt        time.Time
	CheckedAt         time.Time
	NotifyOnFirstDown bool
}

type AlertDecision struct {
	Notify         bool
	PreviousStatus string
}

// AlertStateStore persists alert state for cross-instance de-duplication.
type AlertStateStore interface {
	ClaimAlertEvent(ctx context.Context, state AlertState) (AlertDecision, error)
}
