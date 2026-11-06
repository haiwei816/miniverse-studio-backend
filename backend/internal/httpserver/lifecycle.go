package httpserver

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Serve waits for Shutdown, not merely for the listener to close.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, grace time.Duration) error {
	signal, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		<-signal.Done()
		shutdown, stop := context.WithTimeout(context.Background(), grace)
		defer stop()
		err := server.Shutdown(shutdown)
		if err != nil {
			server.Close()
		}
		done <- err
	}()
	err := server.Serve(listener)
	if err != http.ErrServerClosed {
		return err
	}
	return <-done
}
