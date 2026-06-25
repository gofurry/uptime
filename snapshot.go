package uptime

import (
	"context"
	"errors"
	"time"
)

// Snapshot is the current uptime status payload used by the JSON API.
type Snapshot = StatusResponse

// Snapshot queries the store and returns a fresh status snapshot.
func (u *Uptime) Snapshot(ctx context.Context) (Snapshot, error) {
	if u == nil {
		return Snapshot{}, errors.New("uptime: nil uptime")
	}
	return u.buildStatus(ctx, time.Now())
}

// CachedSnapshot returns a status snapshot through Uptime's in-memory cache.
//
// It is intended for dashboards, custom pages, and user-managed cache layers
// that should not hit the store for every request. When a refresh fails and a
// previous snapshot exists, CachedSnapshot returns the stale snapshot with
// degraded storage status unless Snapshot.DisableStaleIfError is enabled.
func (u *Uptime) CachedSnapshot(ctx context.Context) (Snapshot, error) {
	if u == nil {
		return Snapshot{}, errors.New("uptime: nil uptime")
	}
	if u.config.Snapshot.DisableCache {
		return u.Snapshot(ctx)
	}

	now := time.Now()

	u.snapshotMu.Lock()
	defer u.snapshotMu.Unlock()

	if u.snapshotHasCache && now.Sub(u.snapshotCachedAt) < u.config.Snapshot.CacheTTL {
		return cloneSnapshot(u.snapshotCache), nil
	}

	snapshot, err := u.buildStatus(ctx, now)
	if err != nil {
		if u.snapshotHasCache && !u.config.Snapshot.DisableStaleIfError {
			stale := cloneSnapshot(u.snapshotCache)
			markSnapshotRefreshError(&stale, err, time.Now())
			return stale, nil
		}
		return Snapshot{}, err
	}

	u.snapshotCache = cloneSnapshot(snapshot)
	u.snapshotCachedAt = now
	u.snapshotHasCache = true

	return cloneSnapshot(snapshot), nil
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := in
	if in.Storage.LastErrorAt != nil {
		lastErrorAt := *in.Storage.LastErrorAt
		out.Storage.LastErrorAt = &lastErrorAt
	}
	out.Services = append([]ServiceStatus(nil), in.Services...)
	for i := range out.Services {
		out.Services[i].Daily = append([]DayStatus(nil), in.Services[i].Daily...)
	}
	return out
}

func markSnapshotRefreshError(snapshot *Snapshot, err error, at time.Time) {
	if snapshot == nil || err == nil {
		return
	}
	snapshot.Storage.Status = "degraded"
	snapshot.Storage.LastError = err.Error()
	errorAt := at.UTC()
	snapshot.Storage.LastErrorAt = &errorAt
}
