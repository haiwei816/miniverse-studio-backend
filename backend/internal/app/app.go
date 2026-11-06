// Package app assembles and runs the API process.
package app

import (
	"fmt"
	"net"
	"net/http"

	"miniverse/backend/internal/config"
	"miniverse/backend/internal/httpserver"
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

	server := &http.Server{Handler: httpserver.NewRouter()}
	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}
