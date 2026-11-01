package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateTOTPAndValidate(t *testing.T) {
	setup, err := GenerateTOTP("Test Issuer", "admin@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTP: %v", err)
	}
	if setup.Secret == "" || !strings.HasPrefix(setup.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("unexpected setup payload: %+v", setup)
	}
	if !strings.HasPrefix(setup.QRPNGDataURL, "data:image/png;base64,") {
		t.Fatalf("unexpected QR payload: %s", setup.QRPNGDataURL)
	}

	at := time.Unix(1_700_000_000, 0)
	original := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = original })

	code, err := totp.GenerateCode(setup.Secret, at)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	if !ValidateTOTP(setup.Secret, code) {
		t.Fatal("expected the generated code to validate")
	}
	if ValidateTOTP(setup.Secret, "000000") && code != "000000" {
		t.Fatal("expected a wrong code to be rejected")
	}

	// One period of drift in either direction is accepted, two is not.
	driftedCode, err := totp.GenerateCode(setup.Secret, at.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if !ValidateTOTP(setup.Secret, driftedCode) {
		t.Fatal("expected one period of drift to be accepted")
	}

	staleCode, err := totp.GenerateCode(setup.Secret, at.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if ValidateTOTP(setup.Secret, staleCode) {
		t.Fatal("expected a stale code to be rejected")
	}
}
