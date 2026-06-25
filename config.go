package uptime

import (
	"errors"
	"log"
	"time"
)

const (
	defaultSampleInterval  = 3 * time.Second
	defaultRetentionDays   = 90
	defaultDaysToShow      = 90
	defaultGreenThreshold  = 0.99
	defaultYellowThreshold = 0.95
	defaultUIPath          = "/uptime"
	defaultTheme           = ThemeDark
	defaultLanguage        = LanguageEnglish
	defaultUITitle         = "GoFurry Uptime"
	defaultUIDescription   = "Historical uptime for Go services sharing this storage."
	defaultUIFooter        = "Powered by github.com/gofurry/uptime - MIT License."
	defaultBackground      = BackgroundSolid
)

// Theme controls the dashboard's initial color theme.
type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// Language controls the dashboard's initial UI language.
type Language string

const (
	LanguageEnglish           Language = "en"
	LanguageChineseSimplified Language = "zh-CN"
)

// Background controls the dashboard page background.
type Background string

const (
	BackgroundSolid Background = "solid"
	BackgroundGrid  Background = "grid"
)

// Config controls an uptime recorder.
type Config struct {
	ServiceID          string
	ServiceName        string
	ServiceDescription string

	SampleInterval time.Duration
	RetentionDays  int
	DaysToShow     int

	Timezone *time.Location

	NodeID      int64
	InstanceID  int64
	IDGenerator IDGenerator

	Store Store

	Alert    AlertConfig
	Snapshot SnapshotConfig
	UI       UIConfig
	Logger   Logger
}

// UIConfig controls the built-in status page.
type UIConfig struct {
	Title       string
	Path        string
	Description string
	Footer      string

	DefaultTheme    Theme
	DefaultLanguage Language
	Background      Background

	GreenThreshold  float64
	YellowThreshold float64

	ShowInstanceDetails bool
}

// SnapshotConfig controls the in-memory snapshot cache used by CachedSnapshot
// and the built-in dashboard/API handlers.
type SnapshotConfig struct {
	// CacheTTL controls how long a cached snapshot is reused. It defaults to
	// SampleInterval. Set DisableCache to true for a direct store query on every
	// CachedSnapshot call.
	CacheTTL time.Duration

	// DisableCache makes CachedSnapshot behave like Snapshot.
	DisableCache bool

	// DisableStaleIfError returns store errors instead of a stale snapshot when
	// a refresh fails and a previous snapshot is available.
	DisableStaleIfError bool
}

// Logger is the small logging contract used by uptime.
type Logger interface {
	Printf(format string, args ...any)
}

// IDGenerator generates instance IDs.
type IDGenerator interface {
	NextID() int64
}

// DefaultConfig returns the runtime defaults. ServiceID and Store must still be set.
func DefaultConfig() Config {
	return Config{
		SampleInterval: defaultSampleInterval,
		RetentionDays:  defaultRetentionDays,
		DaysToShow:     defaultDaysToShow,
		Timezone:       time.Local,
		UI: UIConfig{
			Title:           defaultUITitle,
			Path:            defaultUIPath,
			Description:     defaultUIDescription,
			Footer:          defaultUIFooter,
			DefaultTheme:    defaultTheme,
			DefaultLanguage: defaultLanguage,
			Background:      defaultBackground,
			GreenThreshold:  defaultGreenThreshold,
			YellowThreshold: defaultYellowThreshold,
		},
		Logger: log.Default(),
	}
}

func (c Config) normalized() (Config, error) {
	if c.ServiceID == "" {
		return Config{}, ErrMissingServiceID
	}
	if c.Store == nil {
		return Config{}, ErrMissingStore
	}
	if c.ServiceName == "" {
		c.ServiceName = c.ServiceID
	}
	if c.SampleInterval == 0 {
		c.SampleInterval = defaultSampleInterval
	}
	if c.SampleInterval < time.Second {
		return Config{}, errors.New("uptime: sample interval must be at least 1s")
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = defaultRetentionDays
	}
	if c.RetentionDays < 1 {
		return Config{}, errors.New("uptime: retention days must be positive")
	}
	if c.DaysToShow == 0 {
		c.DaysToShow = defaultDaysToShow
	}
	if c.DaysToShow < 1 {
		return Config{}, errors.New("uptime: days to show must be positive")
	}
	if c.DaysToShow > c.RetentionDays {
		c.DaysToShow = c.RetentionDays
	}
	if c.Timezone == nil {
		c.Timezone = time.Local
	}
	if c.Snapshot.CacheTTL == 0 {
		c.Snapshot.CacheTTL = c.SampleInterval
	}
	if !c.Snapshot.DisableCache && c.Snapshot.CacheTTL < time.Second {
		return Config{}, errors.New("uptime: snapshot cache ttl must be at least 1s")
	}
	if c.UI.Path == "" {
		c.UI.Path = defaultUIPath
	}
	c.UI.Path = normalizePath(c.UI.Path)
	if c.UI.Title == "" {
		c.UI.Title = defaultUITitle
	}
	if c.UI.Description == "" {
		c.UI.Description = defaultUIDescription
	}
	if c.UI.Footer == "" {
		c.UI.Footer = defaultUIFooter
	}
	if c.UI.DefaultTheme == "" {
		c.UI.DefaultTheme = defaultTheme
	}
	if c.UI.DefaultTheme != ThemeLight && c.UI.DefaultTheme != ThemeDark {
		return Config{}, errors.New("uptime: ui default theme must be light or dark")
	}
	if c.UI.DefaultLanguage == "" {
		c.UI.DefaultLanguage = defaultLanguage
	}
	if c.UI.DefaultLanguage != LanguageEnglish && c.UI.DefaultLanguage != LanguageChineseSimplified {
		return Config{}, errors.New("uptime: ui default language must be en or zh-CN")
	}
	if c.UI.Background == "" {
		c.UI.Background = defaultBackground
	}
	if c.UI.Background != BackgroundSolid && c.UI.Background != BackgroundGrid {
		return Config{}, errors.New("uptime: ui background must be solid or grid")
	}
	if c.UI.GreenThreshold == 0 {
		c.UI.GreenThreshold = defaultGreenThreshold
	}
	if c.UI.YellowThreshold == 0 {
		c.UI.YellowThreshold = defaultYellowThreshold
	}
	if c.UI.GreenThreshold < c.UI.YellowThreshold {
		return Config{}, errors.New("uptime: green threshold must be greater than or equal to yellow threshold")
	}
	if c.Alert.Hook != nil {
		if _, ok := c.Store.(AlertStateStore); !ok {
			return Config{}, errors.New("uptime: alert hook requires a store that supports alert state")
		}
		if c.Alert.CheckInterval == 0 {
			c.Alert.CheckInterval = c.SampleInterval
		}
		if c.Alert.CheckInterval < time.Second {
			return Config{}, errors.New("uptime: alert check interval must be at least 1s")
		}
	}
	if c.Logger == nil {
		c.Logger = log.Default()
	}
	return c, nil
}

func normalizePath(path string) string {
	if path == "" {
		return defaultUIPath
	}
	if path[0] != '/' {
		path = "/" + path
	}
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}
