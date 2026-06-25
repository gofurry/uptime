package uptime

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gofurry/uptime/internal/ui"
)

// Handler serves the built-in dashboard and JSON API.
func (u *Uptime) Handler() http.Handler {
	return http.HandlerFunc(u.serveHTTP)
}

func (u *Uptime) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	uiPath := u.config.UI.Path
	switch path {
	case uiPath, uiPath + "/":
		u.serveDashboard(w, r)
	case uiPath + "/api/status":
		u.serveStatusJSON(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (u *Uptime) serveStatusJSON(w http.ResponseWriter, r *http.Request) {
	status, err := u.CachedSnapshot(r.Context())
	if err != nil {
		http.Error(w, "uptime status unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(status)
}

func (u *Uptime) serveDashboard(w http.ResponseWriter, r *http.Request) {
	status, err := u.CachedSnapshot(r.Context())
	if err != nil {
		http.Error(w, "uptime dashboard unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	if err := ui.Render(w, toPage(u.config, status)); err != nil {
		http.Error(w, "uptime dashboard render failed", http.StatusInternalServerError)
	}
}

func toPage(config Config, status StatusResponse) ui.Page {
	return ui.Page{
		Title:           config.UI.Title,
		Description:     config.UI.Description,
		Footer:          config.UI.Footer,
		DefaultTheme:    string(config.UI.DefaultTheme),
		DefaultLanguage: string(config.UI.DefaultLanguage),
		Background:      string(config.UI.Background),
		Config: ui.ClientConfig{
			DefaultLanguage: string(config.UI.DefaultLanguage),
			DefaultTheme:    string(config.UI.DefaultTheme),
			RefreshMS:       maxInt64(int64(config.SampleInterval/time.Millisecond), 3000),
			APIPath:         config.UI.Path + "/api/status",
		},
		Status: status,
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
