package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16

	MinPasswordLength = 12
	MaxPasswordLength = 256
)

// ValidatePassword enforces the password policy.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("password must be at most %d bytes", MaxPasswordLength)
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password must not be blank")
	}

	return nil
}

// HashPassword returns an argon2id encoded hash in the PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword compares a password against an encoded argon2id hash. It
// reports false (with a nil error) for malformed hashes so callers can treat
// every mismatch the same way.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, want, err := decodeArgon2(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, nil
	}

	return true, nil
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

func decodeArgon2(encoded string) (params argon2Params, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return params, nil, nil, errors.New("invalid argon2id hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return params, nil, nil, fmt.Errorf("parse argon2 version: %w", err)
	}
	if version != argon2.Version {
		return params, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return params, nil, nil, fmt.Errorf("parse argon2 params: %w", err)
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return params, nil, nil, fmt.Errorf("decode argon2 salt: %w", err)
	}

	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return params, nil, nil, fmt.Errorf("decode argon2 hash: %w", err)
	}
	if len(hash) == 0 {
		return params, nil, nil, errors.New("empty argon2 hash")
	}

	return argon2Params{memory: memory, time: timeCost, threads: threads}, salt, hash, nil
}
