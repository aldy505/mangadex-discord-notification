package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("expected the password to verify")
	}

	ok, err = VerifyPassword(hash, "correct horse battery stapler")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("expected the wrong password to be rejected")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, encoded := range []string{"", "plaintext", "$argon2id$v=19$m=65536$abc$def", "$bcrypt$..."} {
		if _, err := VerifyPassword(encoded, "whatever"); err == nil {
			t.Errorf("expected an error for %q", encoded)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("expected short passwords to be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Error("expected overly long passwords to be rejected")
	}
	if err := ValidatePassword("            "); err == nil {
		t.Error("expected blank passwords to be rejected")
	}
	if err := ValidatePassword("a long enough password"); err != nil {
		t.Errorf("expected a valid password, got %v", err)
	}
}
