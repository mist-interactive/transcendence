package main

import (
	"context"
	"dbBackend/db"
	"dbBackend/handlers"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof" //debug purposes
	"os"
	"strings"
	"time"
)

func main() {
	devMode := os.Getenv("ENV")

	logLevel := slog.LevelDebug
	if devMode == "production" {
		logLevel = slog.LevelInfo
	}
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		switch strings.ToLower(envLevel) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	postgres, err := db.InitDB()
	if err != nil {
		slog.Error("Database connection failed", "error", err)
		os.Exit(1)
	}
	defer postgres.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	err = db.RunMigrations(ctx, postgres)
	if err != nil {
		cancel()
		slog.Error("Database migration failed", "error", err)
		os.Exit(1)
	}

	if devMode != "production" {
		err = db.SeedDevDatabase(ctx, postgres)
		if err != nil {
			slog.Warn("Seeding dev users failed", "error", err)
		}
	}
	cancel()

	mux := http.NewServeMux()
	mux.Handle("/debug/pprof/", http.DefaultServeMux)
	handlers.RegisterRoutes(mux, postgres)

	loggedHandler := handlers.RequestLogger(mux)

	slog.Info("Server starting", "port", 8080, "env", devMode, "log_level", logLevel.String())
	if err := http.ListenAndServe(":8080", loggedHandler); err != nil {
		slog.Error("Server terminated unexpectedly", "error", err)
		os.Exit(1)
	}
}

func dummy() {
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status": "ok", "message": "Go backend is alive!"}`)
	})

	fmt.Println("Server starting on port 8080...")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("%v", err)
	}
}
