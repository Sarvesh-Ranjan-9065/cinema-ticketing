// Package config provides environment-based configuration parsing and validation
// for the cinema ticketing service.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds validated application runtime configuration.
type Config struct {
	// HTTPHost is the host interface on which the HTTP server listens.
	HTTPHost string

	// HTTPPort is the TCP port on which the HTTP server listens.
	HTTPPort int

	// LogLevel is the minimum logging severity (DEBUG, INFO, WARN, ERROR).
	LogLevel string

	// HoldTTL is the duration for which a temporary seat reservation hold remains valid.
	HoldTTL time.Duration

	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string

	// DBMaxOpenConns is the maximum number of open connections to the database.
	DBMaxOpenConns int32

	// DBMaxIdleConns is the maximum number of idle connections retained in the pool.
	DBMaxIdleConns int32

	// DBConnMaxLifetime is the maximum duration a connection may be reused.
	DBConnMaxLifetime time.Duration
}

// Load reads and validates configuration from the environment and secret files.
// It fails fast and aggregates every invalid or missing setting into a single error
// so that operators can address all misconfigurations in a single pass.
func Load() (*Config, error) {
	var errs []error
	cfg := &Config{
		HTTPHost:          "0.0.0.0",
		HTTPPort:          8080,
		LogLevel:          "INFO",
		HoldTTL:           120 * time.Second,
		DBMaxOpenConns:    25,
		DBMaxIdleConns:    10,
		DBConnMaxLifetime: 15 * time.Minute,
	}

	// HTTP Host
	if host := os.Getenv("HTTP_HOST"); host != "" {
		cfg.HTTPHost = strings.TrimSpace(host)
	}

	// HTTP Port
	if portStr := os.Getenv("HTTP_PORT"); portStr != "" {
		port, err := strconv.Atoi(strings.TrimSpace(portStr))
		if err != nil || port < 1 || port > 65535 {
			errs = append(errs, fmt.Errorf("HTTP_PORT must be a valid port number between 1 and 65535, got %q", portStr))
		} else {
			cfg.HTTPPort = port
		}
	}

	// Log Level
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		level = strings.ToUpper(strings.TrimSpace(level))
		switch level {
		case "DEBUG", "INFO", "WARN", "ERROR":
			cfg.LogLevel = level
		default:
			errs = append(errs, fmt.Errorf("LOG_LEVEL must be one of DEBUG, INFO, WARN, ERROR, got %q", level))
		}
	}

	// Hold TTL
	if ttlStr := os.Getenv("HOLD_TTL"); ttlStr != "" {
		ttl, err := time.ParseDuration(strings.TrimSpace(ttlStr))
		if err != nil {
			errs = append(errs, fmt.Errorf("HOLD_TTL must be a valid duration (e.g. 120s, 2m): %w", err))
		} else if ttl < 10*time.Second || ttl > 1*time.Hour {
			errs = append(errs, fmt.Errorf("HOLD_TTL must be between 10s and 1h, got %v", ttl))
		} else {
			cfg.HoldTTL = ttl
		}
	}

	// Database URL (supports direct env var or secret file via DATABASE_URL_FILE)
	dbURL, err := readSecretOrEnv("DATABASE_URL")
	if err != nil {
		errs = append(errs, err)
	} else if dbURL == "" {
		errs = append(errs, errors.New("DATABASE_URL or DATABASE_URL_FILE is required"))
	} else if !strings.HasPrefix(dbURL, "postgres://") && !strings.HasPrefix(dbURL, "postgresql://") {
		errs = append(errs, errors.New("DATABASE_URL must start with postgres:// or postgresql://"))
	} else {
		cfg.DatabaseURL = dbURL
	}

	// Database pool max open connections
	if maxOpenStr := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpenStr != "" {
		maxOpen, err := strconv.ParseInt(strings.TrimSpace(maxOpenStr), 10, 32)
		if err != nil || maxOpen < 1 || maxOpen > math.MaxInt32 {
			errs = append(errs, fmt.Errorf("DB_MAX_OPEN_CONNS must be a positive integer, got %q", maxOpenStr))
		} else {
			cfg.DBMaxOpenConns = int32(maxOpen)
		}
	}

	// Database pool max idle connections
	if maxIdleStr := os.Getenv("DB_MAX_IDLE_CONNS"); maxIdleStr != "" {
		maxIdle, err := strconv.ParseInt(strings.TrimSpace(maxIdleStr), 10, 32)
		if err != nil || maxIdle < 1 || maxIdle > math.MaxInt32 {
			errs = append(errs, fmt.Errorf("DB_MAX_IDLE_CONNS must be a positive integer, got %q", maxIdleStr))
		} else {
			cfg.DBMaxIdleConns = int32(maxIdle)
		}
	}

	if cfg.DBMaxIdleConns > cfg.DBMaxOpenConns {
		errs = append(errs, fmt.Errorf("DB_MAX_IDLE_CONNS (%d) cannot exceed DB_MAX_OPEN_CONNS (%d)", cfg.DBMaxIdleConns, cfg.DBMaxOpenConns))
	}

	// Database connection max lifetime
	if lifetimeStr := os.Getenv("DB_CONN_MAX_LIFETIME"); lifetimeStr != "" {
		lifetime, err := time.ParseDuration(strings.TrimSpace(lifetimeStr))
		if err != nil || lifetime <= 0 {
			errs = append(errs, fmt.Errorf("DB_CONN_MAX_LIFETIME must be a positive duration (e.g. 15m), got %q", lifetimeStr))
		} else {
			cfg.DBConnMaxLifetime = lifetime
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return cfg, nil
}

// readSecretOrEnv resolves a configuration value checking the *_FILE path first,
// falling back to direct environment variable. It never logs secret file contents.
func readSecretOrEnv(envVar string) (string, error) {
	fileVar := envVar + "_FILE"
	rawPath := strings.TrimSpace(os.Getenv(fileVar))
	if rawPath != "" {
		cleanPath := filepath.Clean(rawPath)
		// #nosec G703 -- path to secret file is explicitly provided by operator via environment variable
		content, err := os.ReadFile(cleanPath)
		if err != nil {
			return "", fmt.Errorf("failed to read secret from file specified in %s: %w", fileVar, err)
		}
		return strings.TrimSpace(string(content)), nil
	}

	return strings.TrimSpace(os.Getenv(envVar)), nil
}
