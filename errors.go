package uptime

import "errors"

var (
	ErrMissingServiceID = errors.New("uptime: service id is required")
	ErrMissingStore     = errors.New("uptime: store is required")
)
