package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gofurry/uptime"
	"github.com/gofurry/uptime/store/sqlite"
)

func main() {
	up, err := uptime.New(uptime.Config{
		ServiceID:          "demo-api",
		ServiceName:        "Demo API",
		ServiceDescription: "Example service",
		RetentionDays:      90,
		DaysToShow:         90,
		UI: uptime.UIConfig{
			Title:       "GoFurry Uptime",
			Description: "Demo status page backed by one shared uptime.db.",
			Footer:      "Powered by github.com/gofurry/uptime - MIT License.",
		},
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
