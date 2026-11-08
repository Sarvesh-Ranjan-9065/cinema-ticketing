package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://testuser:testpass@localhost:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.HTTPHost != "0.0.0.0" {
		t.Errorf("expected HTTPHost 0.0.0.0, got %s", cfg.HTTPHost)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("expected HTTPPort 8080, got %d", cfg.HTTPPort)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("expected LogLevel INFO, got %s", cfg.LogLevel)
	}
	if cfg.HoldTTL != 120*time.Second {
		t.Errorf("expected HoldTTL 120s, got %v", cfg.HoldTTL)
	}
	if cfg.DBMaxOpenConns != 25 {
		t.Errorf("expected DBMaxOpenConns 25, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 10 {
		t.Errorf("expected DBMaxIdleConns 10, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 15*time.Minute {
		t.Errorf("expected DBConnMaxLifetime 15m, got %v", cfg.DBConnMaxLifetime)
	}
}

func TestLoad_CustomValidValues(t *testing.T) {
	t.Setenv("HTTP_HOST", "127.0.0.1")
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("HOLD_TTL", "300s")
	t.Setenv("DATABASE_URL", "postgresql://app:dbauth@db:5432/cinema")
	t.Setenv("DB_MAX_OPEN_CONNS", "50")
	t.Setenv("DB_MAX_IDLE_CONNS", "20")
	t.Setenv("DB_CONN_MAX_LIFETIME", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.HTTPHost != "127.0.0.1" {
		t.Errorf("expected HTTPHost 127.0.0.1, got %s", cfg.HTTPHost)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("expected HTTPPort 9090, got %d", cfg.HTTPPort)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("expected LogLevel DEBUG, got %s", cfg.LogLevel)
	}
	if cfg.HoldTTL != 300*time.Second {
		t.Errorf("expected HoldTTL 300s, got %v", cfg.HoldTTL)
	}
	if cfg.DBMaxOpenConns != 50 {
		t.Errorf("expected DBMaxOpenConns 50, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 20 {
		t.Errorf("expected DBMaxIdleConns 20, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 30*time.Minute {
		t.Errorf("expected DBConnMaxLifetime 30m, got %v", cfg.DBConnMaxLifetime)
	}
}

func TestLoad_SecretFileSupport(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "db_connection.txt")
	// #nosec G101 -- dummy connection string used strictly in unit tests
	sampleConn := "postgres://appuser:testpass@db:5432/cinema"

	if err := os.WriteFile(secretFile, []byte(sampleConn+"\n"), 0600); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	t.Setenv("DATABASE_URL_FILE", secretFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected successful load from secret file, got: %v", err)
	}

	if cfg.DatabaseURL != sampleConn {
		t.Errorf("expected DatabaseURL %q, got %q", sampleConn, cfg.DatabaseURL)
	}
}

func TestLoad_SecretFileMissing(t *testing.T) {
	t.Setenv("DATABASE_URL_FILE", "/non/existent/path/secret.txt")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when secret file does not exist, got nil")
	}

	if !strings.Contains(err.Error(), "failed to read secret from file specified in DATABASE_URL_FILE") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_AggregatesMultipleErrors(t *testing.T) {
	t.Setenv("HTTP_PORT", "invalid-port")
	t.Setenv("LOG_LEVEL", "TRACE")
	t.Setenv("HOLD_TTL", "5s")
	t.Setenv("DATABASE_URL", "mysql://invalid:3306/db")
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "10") // exceeds max open

	_, err := Load()
	if err == nil {
		t.Fatal("expected aggregated errors, got nil")
	}

	errMsg := err.Error()
	expectedSubstrings := []string{
		"HTTP_PORT must be a valid port number",
		"LOG_LEVEL must be one of DEBUG, INFO, WARN, ERROR",
		"HOLD_TTL must be between 10s and 1h",
		"DATABASE_URL must start with postgres://",
		"DB_MAX_IDLE_CONNS (10) cannot exceed DB_MAX_OPEN_CONNS (5)",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(errMsg, sub) {
			t.Errorf("expected error message to contain %q, but got:\n%s", sub, errMsg)
		}
	}
}
