package uptime

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gofurry/uptime/internal/timeutil"
)

type StatusResponse struct {
	GeneratedAt           time.Time       `json:"generated_at"`
	SampleIntervalSeconds int64           `json:"sample_interval_seconds"`
	Days                  int             `json:"days"`
	Storage               StorageResponse `json:"storage"`
	Services              []ServiceStatus `json:"services"`
}

type StorageResponse struct {
	Driver      string     `json:"driver"`
	Status      string     `json:"status"`
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

type ServiceStatus struct {
	ID                    string      `json:"id"`
	Name                  string      `json:"name"`
	Description           string      `json:"description,omitempty"`
	LastSeenAt            time.Time   `json:"last_seen_at"`
	CurrentStatus         string      `json:"current_status"`
	SampleIntervalSeconds int64       `json:"sample_interval_seconds"`
	Daily                 []DayStatus `json:"daily"`
}

type DayStatus struct {
	Day                      string  `json:"day"`
	UptimeRate               float64 `json:"uptime_rate"`
	UpSlots                  int     `json:"up_slots"`
	ExpectedSlots            int     `json:"expected_slots"`
	EstimatedDowntimeSeconds int64   `json:"estimated_downtime_seconds"`
	Finalized                bool    `json:"finalized"`
	HasData                  bool    `json:"has_data"`
	Status                   string  `json:"status"`
}

func (u *Uptime) buildStatus(ctx context.Context, now time.Time) (StatusResponse, error) {
	if err := u.runMaintenance(ctx, now, false); err != nil {
		return StatusResponse{}, err
	}

	services, err := u.store.ListServices(ctx)
	if err != nil {
		u.setLastError(err)
		return StatusResponse{}, err
	}

	days := dayRange(now, u.config.DaysToShow, u.config.Timezone)
	fromDay := days[0]
	toDay := days[len(days)-1]
	today := timeutil.DayOf(now, u.config.Timezone)

	dailyRows, err := u.store.QueryDaily(ctx, QueryDailyOptions{FromDay: fromDay, ToDay: toDay})
	if err != nil {
		u.setLastError(err)
		return StatusResponse{}, err
	}
	todayRows, err := u.store.QueryTodaySamples(ctx, QueryTodaySamplesOptions{Day: today})
	if err != nil {
		u.setLastError(err)
		return StatusResponse{}, err
	}

	dailyByService := make(map[string]map[string]DailyStatus)
	for _, row := range dailyRows {
		byDay := dailyByService[row.ServiceID]
		if byDay == nil {
			byDay = make(map[string]DailyStatus)
			dailyByService[row.ServiceID] = byDay
		}
		byDay[row.Day] = row
	}
	todayByService := make(map[string]TodaySampleStatus)
	for _, row := range todayRows {
		todayByService[row.ServiceID] = row
	}

	resp := StatusResponse{
		GeneratedAt:           now.UTC(),
		SampleIntervalSeconds: int64(u.config.SampleInterval / time.Second),
		Days:                  u.config.DaysToShow,
		Storage:               u.storageStatus(),
		Services:              make([]ServiceStatus, 0, len(services)),
	}

	for _, service := range services {
		interval := u.serviceSampleInterval(service)
		serviceStatus := ServiceStatus{
			ID:                    service.ID,
			Name:                  service.Name,
			Description:           service.Description,
			LastSeenAt:            service.LastSeenAt.UTC(),
			CurrentStatus:         currentStatus(now, service.LastSeenAt, interval),
			SampleIntervalSeconds: int64(interval / time.Second),
			Daily:                 make([]DayStatus, 0, len(days)),
		}
		createdDay := timeutil.DayOf(service.CreatedAt, u.config.Timezone)
		for _, day := range days {
			serviceStatus.Daily = append(serviceStatus.Daily, u.dayStatus(service.ID, day, today, createdDay, now, interval, dailyByService, todayByService))
		}
		resp.Services = append(resp.Services, serviceStatus)
	}

	return resp, nil
}

func (u *Uptime) dayStatus(serviceID, day, today, createdDay string, now time.Time, interval time.Duration, daily map[string]map[string]DailyStatus, todayRows map[string]TodaySampleStatus) DayStatus {
	if day < createdDay {
		return DayStatus{
			Day:     day,
			HasData: false,
			Status:  "gray",
		}
	}

	if day == today {
		row := todayRows[serviceID]
		expected := timeutil.ExpectedSlotsSoFar(now, interval, u.config.Timezone)
		return makeDayStatus(day, row.UpSlots, expected, false, true, interval, u.config.UI)
	}

	if byDay := daily[serviceID]; byDay != nil {
		if row, ok := byDay[day]; ok {
			return makeDayStatus(day, row.UpSlots, row.ExpectedSlots, row.Finalized, true, interval, u.config.UI)
		}
	}

	expected := timeutil.ExpectedSlotsForDay(day, interval, u.config.Timezone)
	return makeDayStatus(day, 0, expected, true, true, interval, u.config.UI)
}

func (u *Uptime) serviceSampleInterval(service Service) time.Duration {
	if service.SampleInterval >= time.Second {
		return service.SampleInterval
	}
	return u.config.SampleInterval
}

func makeDayStatus(day string, upSlots, expectedSlots int, finalized, hasData bool, interval time.Duration, ui UIConfig) DayStatus {
	rate := uptimeRate(upSlots, expectedSlots)
	downSlots := expectedSlots - upSlots
	if downSlots < 0 {
		downSlots = 0
	}
	return DayStatus{
		Day:                      day,
		UptimeRate:               rate,
		UpSlots:                  upSlots,
		ExpectedSlots:            expectedSlots,
		EstimatedDowntimeSeconds: int64(time.Duration(downSlots) * interval / time.Second),
		Finalized:                finalized,
		HasData:                  hasData,
		Status:                   colorFor(rate, hasData, ui),
	}
}

func uptimeRate(upSlots, expectedSlots int) float64 {
	if expectedSlots <= 0 {
		return 0
	}
	rate := float64(upSlots) / float64(expectedSlots)
	if rate > 1 {
		return 1
	}
	if rate < 0 || math.IsNaN(rate) {
		return 0
	}
	return rate
}

func colorFor(rate float64, hasData bool, ui UIConfig) string {
	if !hasData {
		return "gray"
	}
	if rate >= ui.GreenThreshold {
		return "green"
	}
	if rate >= ui.YellowThreshold {
		return "yellow"
	}
	return "red"
}

func currentStatus(now, lastSeen time.Time, interval time.Duration) string {
	if lastSeen.IsZero() {
		return "down"
	}
	if now.Sub(lastSeen) <= interval*2 {
		return "up"
	}
	return "down"
}

func dayRange(now time.Time, count int, loc *time.Location) []string {
	if count < 1 {
		count = 1
	}
	today := timeutil.DayOf(now, loc)
	days := make([]string, count)
	for i := 0; i < count; i++ {
		days[count-1-i] = timeutil.AddDays(today, -i, loc)
	}
	return days
}

func (u *Uptime) storageStatus() StorageResponse {
	err, at := u.LastError()
	storage := StorageResponse{
		Driver: storeDriver(u.store),
		Status: "ok",
	}
	if err != nil {
		storage.Status = "degraded"
		storage.LastError = err.Error()
		storage.LastErrorAt = &at
	}
	return storage
}

func storeDriver(store Store) string {
	type named interface {
		Name() string
	}
	if namedStore, ok := store.(named); ok {
		return namedStore.Name()
	}
	return fmt.Sprintf("%T", store)
}
