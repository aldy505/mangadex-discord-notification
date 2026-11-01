package auth

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTPSetup is everything needed to render the enrollment screen.
type TOTPSetup struct {
	Secret       string
	OTPAuthURL   string
	QRPNGDataURL string
}

// GenerateTOTP creates a new TOTP secret for the given account.
func GenerateTOTP(issuer, account string) (TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		SecretSize:  32,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("generate TOTP secret: %w", err)
	}

	image, err := key.Image(320, 320)
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("render TOTP QR code: %w", err)
	}

	var pngBuffer bytes.Buffer
	if err := png.Encode(&pngBuffer, image); err != nil {
		return TOTPSetup{}, fmt.Errorf("encode TOTP QR code: %w", err)
	}

	return TOTPSetup{
		Secret:       key.Secret(),
		OTPAuthURL:   key.URL(),
		QRPNGDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBuffer.Bytes()),
	}, nil
}

// ValidateTOTP checks a six digit code against a secret, allowing one period
// of clock drift in either direction.
func ValidateTOTP(secret, code string) bool {
	valid, err := totp.ValidateCustom(code, secret, timeNow(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false
	}

	return valid
}
