package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jpgomesr/NeuralVault/internal/config"
)

func TestStartHTTPServer_usesConfiguredPort(t *testing.T) {
	// Given
	cfg := &config.Config{Server: config.Server{Port: 9000}}
	expectedErr := errors.New("stop before listening")
	var gotAddr string

	listenAndServe := func(addr string, _ http.Handler) error {
		gotAddr = addr
		return expectedErr
	}

	// When
	err := startHTTPServer(cfg, http.NewServeMux(), listenAndServe)

	// Then
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected listen error %v, got %v", expectedErr, err)
	}
	if gotAddr != ":9000" {
		t.Fatalf("expected server to listen on configured port :9000, got %q", gotAddr)
	}
}
