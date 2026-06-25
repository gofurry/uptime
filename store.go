package uptime

import "context"

// Store persists uptime state. Implementations must be safe for concurrent use.
type Store interface {
	Init(ctx context.Context) error

	UpsertService(ctx context.Context, service Service) error
	UpsertInstance(ctx context.Context, instance Instance) error

	WriteHeartbeat(ctx context.Context, heartbeat Heartbeat) error

	RollupDaily(ctx context.Context, options RollupOptions) error
	Cleanup(ctx context.Context, options CleanupOptions) error

	ListServices(ctx context.Context) ([]Service, error)
	QueryDaily(ctx context.Context, options QueryDailyOptions) ([]DailyStatus, error)
	QueryTodaySamples(ctx context.Context, options QueryTodaySamplesOptions) ([]TodaySampleStatus, error)

	Close() error
}

type RollupOptions struct {
	BeforeDay                  string
	ExpectedSlotsForDay        func(day string) int
	ExpectedSlotsForServiceDay func(serviceID, day string) int
}

type CleanupOptions struct {
	DailyBeforeDay   string
	SamplesBeforeDay string
}

type QueryDailyOptions struct {
	FromDay string
	ToDay   string
}

type QueryTodaySamplesOptions struct {
	Day string
}
