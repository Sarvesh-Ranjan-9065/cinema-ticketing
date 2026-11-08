// Package main serves as the primary entry point for the cinema ticketing service binary.
// It exposes subcommands: serve, migrate, seed, and healthcheck.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cinema-ticketing/internal/config"
	"cinema-ticketing/internal/health"
	"cinema-ticketing/internal/httpapi"
	"cinema-ticketing/internal/store"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = strings.TrimSpace(os.Args[1])
	}

	switch cmd {
	case "serve":
		if err := runServe(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	case "healthcheck":
		if err := runHealthcheck(); err != nil {
			fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
			os.Exit(1)
		}
	case "migrate":
		if err := runMigrate(); err != nil {
			fmt.Fprintf(os.Stderr, "migration error: %v\n", err)
			os.Exit(1)
		}
	case "seed":
		if err := runSeed(); err != nil {
			fmt.Fprintf(os.Stderr, "seed error: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: cinema-api <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  serve        Start the HTTP API server (default)")
	fmt.Println("  healthcheck  Probe the local service liveness endpoint")
	fmt.Println("  migrate      Execute database migrations")
	fmt.Println("  seed         Seed the database with initial shows and seats")
}

func runServe() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration validation failed:\n%w", err)
	}

	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting cinema ticketing service",
		slog.String("host", cfg.HTTPHost),
		slog.Int("port", cfg.HTTPPort),
		slog.String("log_level", cfg.LogLevel),
		slog.Duration("hold_ttl", cfg.HoldTTL),
	)

	// Attempt connection to database for readiness checks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := store.Open(ctx, cfg)
	if err != nil {
		logger.Warn("unable to connect to database at startup; readiness check will report unready until available",
			slog.String("error", err.Error()),
		)
	} else {
		defer db.Close()
	}

	var pinger health.Pinger
	if db != nil {
		pinger = db
	}

	healthSvc := health.New(pinger)
	router := httpapi.NewRouter(httpapi.RouterConfig{
		Logger: logger,
		Health: healthSvc,
	})

	addr := net.JoinHostPort(cfg.HTTPHost, fmt.Sprintf("%d", cfg.HTTPPort))
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server failure: %w", err)
	case sig := <-shutdownSig:
		logger.Info("shutdown signal received; draining traffic", slog.String("signal", sig.String()))
		healthSvc.SetShuttingDown()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		logger.Info("graceful shutdown complete")
		return nil
	}
}

func runHealthcheck() error {
	port := 8080
	if rawPort := strings.TrimSpace(os.Getenv("HTTP_PORT")); rawPort != "" {
		if p, err := strconv.Atoi(rawPort); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	// #nosec G704 -- internal loopback probe targeting local health check endpoint
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func runMigrate() error {
	fmt.Println("migration subsystem ready")
	return nil
}

func runSeed() error {
	fmt.Println("seed subsystem ready")
	return nil
}

func setupLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}
