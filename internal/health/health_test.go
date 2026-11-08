package health

import (
	"context"
	"errors"
	"testing"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

func TestHealth_IsLive(t *testing.T) {
	svc := New(nil)
	if !svc.IsLive() {
		t.Fatal("expected IsLive to return true")
	}
}

func TestHealth_CheckReady(t *testing.T) {
	tests := []struct {
		name         string
		pinger       Pinger
		shuttingDown bool
		wantErr      bool
		errContains  string
	}{
		{
			name:         "nil pinger ready",
			pinger:       nil,
			shuttingDown: false,
			wantErr:      false,
		},
		{
			name:         "successful pinger ready",
			pinger:       &mockPinger{err: nil},
			shuttingDown: false,
			wantErr:      false,
		},
		{
			name:         "failed pinger not ready",
			pinger:       &mockPinger{err: errors.New("db connection refused")},
			shuttingDown: false,
			wantErr:      true,
			errContains:  "db connection refused",
		},
		{
			name:         "shutting down not ready even if pinger passes",
			pinger:       &mockPinger{err: nil},
			shuttingDown: true,
			wantErr:      true,
			errContains:  "service is shutting down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(tt.pinger)
			if tt.shuttingDown {
				svc.SetShuttingDown()
			}

			err := svc.CheckReady(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && err.Error() != tt.errContains {
					t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
