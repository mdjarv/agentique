package main

import (
	"net/http"
	"testing"
	"time"
)

func TestValidateAuthDisabledAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9201", "[::1]:9201", "localhost:9201"} {
		if err := validateAuthDisabledAddress(addr); err != nil {
			t.Errorf("loopback address %q rejected: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:9201", ":9201", "192.168.1.20:9201", "zbook:9201"} {
		if err := validateAuthDisabledAddress(addr); err == nil {
			t.Errorf("reachable address %q accepted with authentication disabled", addr)
		}
	}
}

func TestHTTPServerHasResourceTimeouts(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %v, want 30s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 2*time.Minute {
		t.Fatalf("IdleTimeout = %v, want 2m", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want 65536", srv.MaxHeaderBytes)
	}
}
