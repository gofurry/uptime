package uptime

import "net/http"

// Middleware is a no-op net/http middleware in v0.1.0.
//
// Heartbeats are written by the background recorder, so uptime works even when
// the middleware is not installed on the business handler.
func (u *Uptime) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
