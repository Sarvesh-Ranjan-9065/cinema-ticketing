package booking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateTicketID produces a unique cryptographically random ticket identifier.
// The resulting string is prefixed with 'TKT-' followed by 16 uppercase hexadecimal characters.
func GenerateTicketID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed generating random ticket bytes: %w", err)
	}
	return fmt.Sprintf("TKT-%s", hex.EncodeToString(bytes)), nil
}
