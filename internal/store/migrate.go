package store

import (
	"errors"
	"fmt"
	"strings"

	"cinema-ticketing/migrations"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// ensurePgxScheme converts postgres:// or postgresql:// to pgx5:// for the golang-migrate pgx/v5 driver.
func ensurePgxScheme(dbURL string) string {
	if strings.HasPrefix(dbURL, "postgres://") {
		return "pgx5://" + strings.TrimPrefix(dbURL, "postgres://")
	}
	if strings.HasPrefix(dbURL, "postgresql://") {
		return "pgx5://" + strings.TrimPrefix(dbURL, "postgresql://")
	}
	return dbURL
}

// MigrateUp applies all pending database schema migrations.
func MigrateUp(databaseURL string) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration up failed: %w", err)
	}

	return nil
}

// MigrateDown rolls back all applied migrations.
func MigrateDown(databaseURL string) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration down failed: %w", err)
	}

	return nil
}

// MigrationVersion reports the current migration version and dirty status.
func MigrationVersion(databaseURL string) (uint, bool, error) {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		_, _ = m.Close()
	}()

	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("failed to retrieve migration version: %w", err)
	}

	return v, dirty, nil
}

func newMigrator(databaseURL string) (*migrate.Migrate, error) {
	d, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	connURL := ensurePgxScheme(databaseURL)
	m, err := migrate.NewWithSourceInstance("iofs", d, connURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migrator: %w", err)
	}

	return m, nil
}
