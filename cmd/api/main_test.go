package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	// Ensure printUsage runs without panicking
	printUsage()
}

func TestSetupLogger(t *testing.T) {
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR", "UNKNOWN"}
	for _, lvl := range levels {
		logger := setupLogger(lvl)
		if logger == nil {
			t.Errorf("expected non-nil logger for level %s", lvl)
		}
	}
}

func TestRunHealthcheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Extract port from test server URL
	u := ts.URL
	port := u[len("http://127.0.0.1:"):]

	t.Setenv("HTTP_PORT", port)

	if err := runHealthcheck(); err != nil {
		t.Fatalf("expected healthcheck to succeed, got: %v", err)
	}
}

func TestRunHealthcheck_Failure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	port := ts.URL[len("http://127.0.0.1:"):]
	t.Setenv("HTTP_PORT", port)

	if err := runHealthcheck(); err == nil {
		t.Fatal("expected healthcheck error on 503 response, got nil")
	}
}

func TestRunServe_ConfigError(t *testing.T) {
	// Missing DATABASE_URL causes runServe to return validation error
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", "")

	if err := runServe(); err == nil {
		t.Fatal("expected configuration error, got nil")
	}
}

func TestRunMigrate_ConfigError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", "")
	if err := runMigrate(); err == nil {
		t.Fatal("expected error on missing config, got nil")
	}
}

func TestRunSeed_ConfigError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", "")
	if err := runSeed(); err == nil {
		t.Fatal("expected error on missing config, got nil")
	}
}

func TestRunHealthcheck_UnreachablePort(t *testing.T) {
	// Port 1 is reserved and typically not running an HTTP server
	t.Setenv("HTTP_PORT", "1")
	if err := runHealthcheck(); err == nil {
		t.Fatal("expected error connecting to unreachable port, got nil")
	}
}

func TestRunServe_InvalidSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "missing.txt")
	t.Setenv("DATABASE_URL_FILE", invalidFile)

	if err := runServe(); err == nil {
		t.Fatal("expected runServe to fail when secret file is missing, got nil")
	}
}
