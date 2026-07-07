package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jpgomesr/NeuralVault/internal/config"
)

func TestStartHTTPServer_usesConfiguredPort(t *testing.T) {
	// Given
	cfg := &config.Config{Server: config.Server{Port: 9000}}
	expectedErr := errors.New("stop before listening")
	var gotAddr string

	serve := func(s *http.Server) error {
		gotAddr = s.Addr
		return expectedErr
	}
	shutdown := func(*http.Server, context.Context) error { return nil }

	// When
	err := startHTTPServer(context.Background(), cfg, http.NewServeMux(), serve, shutdown)

	// Then
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected serve error %v, got %v", expectedErr, err)
	}
	if gotAddr != ":9000" {
		t.Fatalf("expected server to listen on configured port :9000, got %q", gotAddr)
	}
}

func TestStartHTTPServer_serverClosedIsNotAnError(t *testing.T) {
	// Given a serve that returns http.ErrServerClosed (normal shutdown).
	cfg := &config.Config{Server: config.Server{Port: 9000}}
	serve := func(*http.Server) error { return http.ErrServerClosed }
	shutdown := func(*http.Server, context.Context) error { return nil }

	// When
	err := startHTTPServer(context.Background(), cfg, http.NewServeMux(), serve, shutdown)

	// Then
	if err != nil {
		t.Fatalf("expected nil error on ErrServerClosed, got %v", err)
	}
}

func TestStartHTTPServer_gracefulShutdownOnContextCancel(t *testing.T) {
	// Given a cancelled context, serve blocks until shutdown signals it.
	cfg := &config.Config{Server: config.Server{Port: 9000, ShutdownTimeout: time.Second}}
	ctx, cancel := context.WithCancel(context.Background())

	served := make(chan struct{})
	shutdownCalled := false
	serve := func(*http.Server) error {
		<-served
		return http.ErrServerClosed
	}
	shutdown := func(_ *http.Server, _ context.Context) error {
		shutdownCalled = true
		close(served)
		return nil
	}

	cancel() // trigger the shutdown path before starting

	// When
	err := startHTTPServer(ctx, cfg, http.NewServeMux(), serve, shutdown)

	// Then
	if err != nil {
		t.Fatalf("expected nil error on graceful shutdown, got %v", err)
	}
	if !shutdownCalled {
		t.Fatal("expected shutdown to be called when context is cancelled")
	}
}
