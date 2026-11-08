package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"cinema-ticketing/internal/config"
)

func TestOpen_InvalidURL(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL:       "invalid-database-url",
		DBMaxOpenConns:    10,
		DBMaxIdleConns:    2,
		DBConnMaxLifetime: 5 * time.Minute,
	}

	_, err := Open(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error with invalid database URL, got nil")
	}

	if !strings.Contains(err.Error(), "failed to parse database configuration") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDB_PingNil(t *testing.T) {
	var db *DB
	err := db.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error pinging nil DB, got nil")
	}
	if !strings.Contains(err.Error(), "database pool is not initialized") {
		t.Errorf("unexpected error message: %v", err)
	}
}
