package auth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// timeNow is indirected so tests can control TOTP validation.
var timeNow = time.Now

// NewWebAuthn builds the relying party from the configured public base URL.
// Passkeys only work when the origin matches exactly, which is why the origin
// is taken from configuration rather than from request headers.
func NewWebAuthn(publicBaseURL *url.URL, displayName string) (*webauthn.WebAuthn, error) {
	if publicBaseURL == nil || publicBaseURL.Host == "" {
		return nil, fmt.Errorf("public base URL is required for passkeys")
	}

	origin := publicBaseURL.Scheme + "://" + publicBaseURL.Host

	return webauthn.New(&webauthn.Config{
		RPID:          publicBaseURL.Hostname(),
		RPDisplayName: displayName,
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

// WebAuthnUser adapts an admin row to the webauthn.User interface.
type WebAuthnUser struct {
	ID          string
	Email       string
	Credentials []webauthn.Credential
}

func (u *WebAuthnUser) WebAuthnID() []byte {
	return UserHandle(u.ID)
}

func (u *WebAuthnUser) WebAuthnName() string {
	return u.Email
}

func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.Email
}

func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// MarshalCredential serializes a credential for storage.
func MarshalCredential(credential *webauthn.Credential) (string, error) {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("marshal passkey: %w", err)
	}

	return string(encoded), nil
}

// UnmarshalCredential restores a credential from storage.
func UnmarshalCredential(data string) (webauthn.Credential, error) {
	var credential webauthn.Credential
	if err := json.Unmarshal([]byte(data), &credential); err != nil {
		return webauthn.Credential{}, fmt.Errorf("unmarshal passkey: %w", err)
	}

	return credential, nil
}

// MarshalSessionData serializes an in-flight ceremony for storage.
func MarshalSessionData(data *webauthn.SessionData) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal webauthn session: %w", err)
	}

	return string(encoded), nil
}

// UnmarshalSessionData restores an in-flight ceremony from storage.
func UnmarshalSessionData(data string) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return webauthn.SessionData{}, fmt.Errorf("unmarshal webauthn session: %w", err)
	}

	return session, nil
}
