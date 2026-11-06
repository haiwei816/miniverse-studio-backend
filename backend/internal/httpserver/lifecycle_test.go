package httpserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownWaitsForInflightRequest(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	started, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; w.Write([]byte("complete")) })}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, server, listener, time.Second) }()
	response := make(chan string, 1)
	go func() {
		r, e := http.Get("http://" + listener.Addr().String())
		if e != nil {
			response <- e.Error()
			return
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		response <- string(b)
	}()
	<-started
	cancel()
	select {
	case e := <-done:
		t.Fatalf("returned before request completed: %v", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if v := <-response; v != "complete" {
		t.Fatal(v)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
