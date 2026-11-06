package main

import (
	"fmt"
	"os"

	"miniverse/backend/internal/app"
	"miniverse/backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	return app.Run(cfg)
}
