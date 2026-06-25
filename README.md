# uptime

Tiny uptime history middleware for Go `net/http`.

- Records heartbeat samples in the background
- Shows daily uptime bars for the last N days
- Uses SQLite for single-machine deployments, or PostgreSQL for shared multi-instance deployments
- Works without Prometheus, Grafana, or an external monitor
- Complements `gofurry/monitor`: monitor shows current runtime state, uptime shows historical availability

## Install

```bash
go get github.com/gofurry/uptime
```

## Quick Start

```go
package main

import (
	"log"
	"net/http"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/store/sqlite"
)

func main() {
	up, err := uptime.New(uptime.Config{
		ServiceID:   "demo-api",
		ServiceName: "Demo API",
		Store: sqlite.New(sqlite.Config{
			Path: "./uptime.db",
		}),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer up.Close()

	mux := http.NewServeMux()
	mux.Handle("/uptime", up.Handler())
	mux.Handle("/uptime/", up.Handler())
	mux.Handle("/", up.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})))

	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

Open:

- `http://localhost:8080/uptime`
- `http://localhost:8080/uptime/api/status`

## Demo Data

Generate a local `uptime.db` with multiple services, multiple instances, and 90 days of history:

```bash
go run ./cmd/uptime-demo-data -path ./uptime.db -reset=true
go run ./examples/basic
```

The example service writes its own heartbeat while the dashboard reads every service stored in the same database file.

## Dashboard Only

`uptime` does not depend on business requests. Heartbeats are written by a background ticker, so this is valid:

```go
mux.Handle("/uptime", up.Handler())
mux.Handle("/uptime/", up.Handler())
```

`Middleware` is a pass-through adapter in v0.1.0. It is provided for normal `net/http` integration style and future request-aware features.

## PostgreSQL

Use `store/postgres` when multiple service instances need to share one central uptime database:

```go
package main

import (
	"log"
	"net/http"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/store/postgres"
)

func main() {
	up, err := uptime.New(uptime.Config{
		ServiceID:   "demo-api",
		ServiceName: "Demo API",
		Store: postgres.New(postgres.Config{
			Host:        "127.0.0.1",
			Port:        5432,
			Database:    "postgres",
			Username:    "postgres",
			Password:    "password",
			SSLMode:     "disable",
			Schema:      "public",
			TablePrefix: "uptime_",
		}),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer up.Close()

	mux := http.NewServeMux()
	mux.Handle("/uptime", up.Handler())
	mux.Handle("/uptime/", up.Handler())
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

You can also pass `postgres.Config{DSN: "postgres://..."}`. The PostgreSQL store creates its schema, tables, and indexes automatically. The default table names are `uptime_services`, `uptime_instances`, `uptime_samples`, and `uptime_daily`; use `TablePrefix` or `Tables` for custom names.

## Configuration

`ServiceID` and `Store` are required. The core package does not import SQLite automatically.

Defaults:

| Field | Default |
| --- | --- |
| `SampleInterval` | `3 * time.Second` |
| `RetentionDays` | `90` |
| `DaysToShow` | `90` |
| `Timezone` | `time.Local` |
| `UI.Title` | `GoFurry Uptime` |
| `UI.Description` | `Historical uptime for Go services sharing this storage.` |
| `UI.Footer` | `Powered by github.com/gofurry/uptime - MIT License.` |
| `UI.DefaultTheme` | `dark` |
| `UI.DefaultLanguage` | `en` |
| `UI.Background` | `solid` |
| green threshold | `99%` |
| yellow threshold | `95%` |

`ServiceID` should be a stable business identity such as `api`, `worker`, or `gofurry-api`. Do not generate a new service ID on each start, or history will be split across services.

## How It Works

The process writes one `up` heartbeat every sample interval. Missing heartbeat slots are treated as downtime.

For a 3 second interval:

```text
expected slots per normal day = 24 * 60 * 60 / 3 = 28800
uptime rate = distinct up slots / expected slots
```

For multiple instances of the same service, a slot is up when any instance writes a heartbeat for that slot.

When multiple services share the same store, the dashboard/API calculate current status, today's expected slots, missing-day expected slots, and estimated downtime with each service's stored sample interval.

Raw samples are kept for today and yesterday. Older samples are rolled up into daily snapshots and then cleaned up.

## SQLite Notes

The SQLite store uses the pure-Go `modernc.org/sqlite` driver and configures:

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
```

SQLite is intended for one machine. Multiple local processes may share the same database file, but network filesystems such as NFS are not recommended.

## PostgreSQL Notes

The PostgreSQL store uses `github.com/jackc/pgx/v5/stdlib` through `database/sql`. It is intended for shared deployments where multiple processes or machines write to the same uptime store.

Configurable PostgreSQL fields:

| Field | Default |
| --- | --- |
| `DSN` | empty |
| `Host` | `localhost` |
| `Port` | `5432` |
| `Database` | `postgres` |
| `SSLMode` | `disable` |
| `Schema` | `public` |
| `TablePrefix` | `uptime_` |
| `MaxOpenConns` | `5` |
| `MaxIdleConns` | `2` |

`Username` is required when `DSN` is empty. `Tables` can override each table name individually.

## Security

The dashboard is public by default. Put authentication, IP allowlists, or reverse proxy rules outside this package when the endpoint is exposed beyond trusted networks.

The middleware does not read request bodies, capture response bodies, log sensitive headers, or store request contexts.

## Dashboard

The built-in page has no external assets. The frontend is maintained as embedded `page.html`, `style.css`, and `app.js` files under `internal/ui`, matching the structure used by `gofurry/monitor`.

It supports light and dark theme modes, plus English and simplified Chinese labels. The last selected theme and language are stored in browser local storage.

Daily bars use a custom hover card instead of the browser's native tooltip. The card is anchored to the active bar and centered below it.

## Concurrency

`Uptime` instances and the SQLite store are safe for concurrent use after construction. Runtime heartbeat failures are recorded in memory and shown as degraded storage status; they do not affect business handlers.

## Compatibility

This module targets Go 1.22.

SQLite and PostgreSQL stores are provided. Additional databases can be added through the existing `Store` interface.
