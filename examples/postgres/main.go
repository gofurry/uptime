package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/store/postgres"
)

func main() {
	dsn := os.Getenv("UPTIME_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("set UPTIME_POSTGRES_DSN, for example postgres://postgres:password@localhost:5432/postgres?sslmode=disable")
	}

	up, err := uptime.New(uptime.Config{
		ServiceID:          "demo-api",
		ServiceName:        "Demo API",
		ServiceDescription: "Example service backed by PostgreSQL",
		Store: postgres.New(postgres.Config{
			DSN:         dsn,
			Schema:      envOr("UPTIME_POSTGRES_SCHEMA", "public"),
			TablePrefix: envOr("UPTIME_POSTGRES_TABLE_PREFIX", "uptime_"),
		}),
		UI: uptime.UIConfig{
			Title:       "GoFurry Uptime",
			Description: "Demo status page backed by PostgreSQL.",
		},
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

	addr := os.Getenv("UPTIME_EXAMPLE_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	displayAddr := addr
	if strings.HasPrefix(addr, ":") {
		displayAddr = "localhost" + addr
	}
	log.Printf("listening on http://%s", displayAddr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
