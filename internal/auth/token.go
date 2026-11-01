package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// NewToken returns a URL safe random token and the sha256 hash that should be
// stored in the database.
func NewToken(byteLength int) (raw string, hash string, err error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)

	return raw, HashToken(raw), nil
}

// HashToken hashes a token for storage and lookup.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}

// UserHandle derives the stable WebAuthn user handle for an admin id.
func UserHandle(adminID string) []byte {
	sum := sha256.Sum256([]byte(adminID))

	return sum[:]
}
