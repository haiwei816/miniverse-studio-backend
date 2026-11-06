// Package app assembles and runs the API process.
package app

import (
	"context"
	"fmt"
	"miniverse/backend/internal/database"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"miniverse/backend/internal/config"
	"miniverse/backend/internal/httpserver"
	"miniverse/backend/internal/service"
)

func Run(cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	db, err := database.Open(os.Getenv("MYSQL_DSN"))
	if err != nil {
		return fmt.Errorf("database configuration invalid")
	}
	defer db.Close()
	router := httpserver.NewRouter()
	mux := http.NewServeMux()
	mux.Handle("/", router)
	svc := service.New(db, os.Getenv("APP_ENV") == "sandbox")
	if err := svc.Initialize(); err != nil {
		return err
	}
	svc.Routes(mux)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if database.Ready(ctx, db) != nil {
			http.Error(w, `{"error":"not_ready"}`, 503)
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	server := &http.Server{Handler: httpserver.Logging(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := httpserver.Serve(ctx, server, listener, 10*time.Second); err != nil {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}
