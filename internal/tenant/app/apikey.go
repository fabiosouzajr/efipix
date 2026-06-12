package app

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey returns the sha256 hex of a raw API key, matching the DB digest().
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
