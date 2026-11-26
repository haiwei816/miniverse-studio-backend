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

	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		return fmt.Errorf("database configuration invalid")
	}
	defer db.Close()
	router := httpserver.NewRouter()
	mux := http.NewServeMux()
	mux.Handle("/", router)
	svc := service.New(db, cfg.IsSandbox())
	svc.TrustedProxies = cfg.TrustedProxies
	svc.RuleCoreAddr = cfg.RuleCoreAddr
	svc.SandboxPlatformSecret = cfg.SandboxPlatformSecret
	svc.CompanionChatURL = cfg.CompanionChatURL
	svc.CompanionAPIKey = cfg.CompanionAPIKey
	svc.CompanionModel = cfg.CompanionModel
	svc.OpsAgentProtocol = cfg.OpsAgentProtocol
	svc.OpsAgentURL = cfg.OpsAgentURL
	svc.OpsAgentAPIKey = cfg.OpsAgentAPIKey
	svc.OpsAgentModel = cfg.OpsAgentModel
	svc.OpsAgentSecretKey = cfg.OpsAgentSecretKey
	svc.MetricsBearerToken = cfg.MetricsBearerToken
	svc.RedisAddr = cfg.RedisAddr
	svc.KafkaBrokers = cfg.KafkaBrokers
	svc.PersistOpsEvents = true
	defer svc.Close()
	if err := svc.Initialize(); err != nil {
		return err
	}
	svc.Routes(mux)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if database.Ready(ctx, db) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"not_ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	server := &http.Server{Handler: httpserver.Logging(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopCleanup := database.StartCleanup(ctx, db)
	defer stopCleanup()
	if err := httpserver.Serve(ctx, server, listener, 10*time.Second); err != nil {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}
