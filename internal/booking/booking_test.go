package booking

import (
	"strings"
	"testing"
)

func TestGenerateTicketID(t *testing.T) {
	ticket1, err := GenerateTicketID()
	if err != nil {
		t.Fatalf("expected no error generating ticket, got: %v", err)
	}

	if !strings.HasPrefix(ticket1, "TKT-") {
		t.Errorf("expected ticket to start with 'TKT-', got %s", ticket1)
	}

	if len(ticket1) != 20 { // "TKT-" (4) + 16 hex chars = 20
		t.Errorf("expected ticket length 20, got %d (%s)", len(ticket1), ticket1)
	}

	ticket2, err := GenerateTicketID()
	if err != nil {
		t.Fatalf("expected no error generating second ticket, got: %v", err)
	}

	if ticket1 == ticket2 {
		t.Errorf("expected distinct ticket IDs, got identical %s", ticket1)
	}
}

func TestModelStatusConstants(t *testing.T) {
	if StatusHeld != "HELD" || StatusConfirmed != "CONFIRMED" || StatusCancelled != "CANCELLED" || StatusExpired != "EXPIRED" {
		t.Errorf("unexpected reservation status constants")
	}

	if SeatAvailable != "AVAILABLE" || SeatHeld != "HELD" || SeatConfirmed != "CONFIRMED" {
		t.Errorf("unexpected seat status constants")
	}
}
