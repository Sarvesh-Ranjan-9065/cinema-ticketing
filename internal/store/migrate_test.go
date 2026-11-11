package store

import (
	"testing"
)

func TestEnsurePgxScheme(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"postgres://user:pass@localhost:5432/db", "pgx5://user:pass@localhost:5432/db"},
		{"postgresql://user:pass@localhost:5432/db", "pgx5://user:pass@localhost:5432/db"},
		{"pgx5://user:pass@localhost:5432/db", "pgx5://user:pass@localhost:5432/db"},
		{"custom://localhost:5432/db", "custom://localhost:5432/db"},
	}

	for _, tt := range tests {
		result := ensurePgxScheme(tt.input)
		if result != tt.expected {
			t.Errorf("ensurePgxScheme(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestMigrateUp_InvalidURL(t *testing.T) {
	err := MigrateUp("invalid-url")
	if err == nil {
		t.Fatal("expected error with invalid database URL, got nil")
	}
}
