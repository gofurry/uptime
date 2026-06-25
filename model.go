package uptime

import "time"

// Service is the logical service identity shown on the dashboard.
type Service struct {
	ID             string
	Name           string
	Description    string
	CreatedAt      time.Time
	LastSeenAt     time.Time
	SampleInterval time.Duration
}

// Instance describes one process lifetime of a service.
type Instance struct {
	ID         int64
	ServiceID  string
	Hostname   string
	PID        int
	StartedAt  time.Time
	LastSeenAt time.Time
}

// Heartbeat records that one instance was alive during a day slot.
type Heartbeat struct {
	ServiceID  string
	InstanceID int64
	Day        string
	Slot       int64
	SeenAt     time.Time
}

// DailyStatus is a finalized service-level day snapshot.
type DailyStatus struct {
	ServiceID     string
	Day           string
	UpSlots       int
	ExpectedSlots int
	UptimeRate    float64
	Finalized     bool
}

// TodaySampleStatus is the current raw service-level summary for one day.
type TodaySampleStatus struct {
	ServiceID string
	Day       string
	UpSlots   int
}
