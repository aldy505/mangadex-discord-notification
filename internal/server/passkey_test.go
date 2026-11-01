package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/pquerna/otp/totp"
)

// virtualAuthenticator is a minimal software authenticator used to drive the
// real WebAuthn ceremonies through the HTTP API. It mirrors what a platform
// authenticator does: build authenticator data, sign the client data hash, and
// hand the encoded result back to the relying party.
type virtualAuthenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	userHandle   []byte
	signCount    uint32
}

func newVirtualAuthenticator(t *testing.T) *virtualAuthenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate authenticator key: %v", err)
	}

	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		t.Fatalf("generate credential ID: %v", err)
	}

	return &virtualAuthenticator{key: key, credentialID: credentialID}
}

func (a *virtualAuthenticator) coseKey(t *testing.T) []byte {
	t.Helper()

	x := make([]byte, 32)
	y := make([]byte, 32)
	a.key.PublicKey.X.FillBytes(x)
	a.key.PublicKey.Y.FillBytes(y)

	encoded, err := cbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: x,
		-3: y,
	})
	if err != nil {
		t.Fatalf("encode COSE key: %v", err)
	}

	return encoded
}

func b64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeB64url(t *testing.T, value string) []byte {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode base64url %q: %v", value, err)
	}

	return decoded
}

func (a *virtualAuthenticator) authenticatorData(t *testing.T, relyingPartyID string, flags byte, attested bool) []byte {
	t.Helper()

	rpIDHash := sha256.Sum256([]byte(relyingPartyID))

	buffer := make([]byte, 0, 512)
	buffer = append(buffer, rpIDHash[:]...)
	buffer = append(buffer, flags)
	buffer = binary.BigEndian.AppendUint32(buffer, a.signCount)

	if !attested {
		return buffer
	}

	buffer = append(buffer, make([]byte, 16)...) // AAGUID
	buffer = binary.BigEndian.AppendUint16(buffer, uint16(len(a.credentialID)))
	buffer = append(buffer, a.credentialID...)
	buffer = append(buffer, a.coseKey(t)...)

	return buffer
}

type creationOptions struct {
	Challenge    string
	RelyingParty string
	UserID       []byte
}

func parseCreationOptions(t *testing.T, payload map[string]any) creationOptions {
	t.Helper()

	options, ok := payload["options"].(map[string]any)
	if !ok {
		t.Fatalf("missing options in %v", payload)
	}

	challenge, _ := options["challenge"].(string)
	rp, _ := options["rp"].(map[string]any)
	user, _ := options["user"].(map[string]any)

	rpID, _ := rp["id"].(string)
	userID, _ := user["id"].(string)

	return creationOptions{
		Challenge:    challenge,
		RelyingParty: rpID,
		UserID:       decodeB64url(t, userID),
	}
}

func clientDataJSON(t *testing.T, ceremony, challenge, origin string) []byte {
	t.Helper()

	encoded, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("encode client data: %v", err)
	}

	return encoded
}

// TestPasskeyEnrollmentAndLogin drives registration and assertion through the
// HTTP API exactly as a browser authenticator would.
func TestPasskeyEnrollmentAndLogin(t *testing.T) {
	env := newTestEnv(t)
	origin := "http://localhost"

	inviteToken := env.createInvite("passkey@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)
	if accept["stage"] != "enroll" {
		t.Fatalf("stage = %v", accept["stage"])
	}

	// --- registration -------------------------------------------------------
	begin := env.decode(env.do(env.client, http.MethodPost, "/api/auth/passkey/register/begin",
		map[string]any{"name": "Virtual Key"}, csrf))
	options := parseCreationOptions(t, begin)

	authenticator := newVirtualAuthenticator(t)
	authenticator.userHandle = options.UserID

	attestationData := authenticator.authenticatorData(t, options.RelyingParty, 0x45 /* UP|UV|AT */, true)
	attestation, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": attestationData,
	})
	if err != nil {
		t.Fatalf("encode attestation object: %v", err)
	}

	registration := env.do(env.client, http.MethodPost, "/api/auth/passkey/register/finish", map[string]any{
		"id":                      b64url(authenticator.credentialID),
		"rawId":                   b64url(authenticator.credentialID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64url(clientDataJSON(t, "webauthn.create", options.Challenge, origin)),
			"attestationObject": b64url(attestation),
			"transports":        []string{"internal"},
		},
	}, csrf)
	if registration.status != http.StatusOK {
		t.Fatalf("registration status = %d (%s)", registration.status, registration.body)
	}
	if stage := env.decode(registration)["stage"]; stage != "full" {
		t.Fatalf("stage after registration = %v", stage)
	}

	account := env.decode(env.do(env.client, http.MethodGet, "/api/account", nil, ""))
	passkeys, _ := account["passkeys"].([]any)
	if len(passkeys) != 1 {
		t.Fatalf("passkeys = %d, want 1", len(passkeys))
	}
	if name := passkeys[0].(map[string]any)["name"]; name != "Virtual Key" {
		t.Errorf("passkey name = %v", name)
	}

	// The passkey is the only factor, so it cannot be removed.
	session := env.decode(env.do(env.client, http.MethodGet, "/api/session", nil, ""))
	csrf = session["csrf_token"].(string)
	if removed := env.do(env.client, http.MethodDelete, "/api/account/passkeys/"+b64url(authenticator.credentialID), nil, csrf); removed.status != http.StatusConflict {
		t.Fatalf("removing the last factor status = %d, want 409", removed.status)
	}

	// --- login --------------------------------------------------------------
	env.do(env.client, http.MethodPost, "/api/auth/logout", nil, csrf)

	jar := newTestClient(t)
	login := env.decode(env.do(jar, http.MethodPost, "/api/auth/login",
		map[string]string{"email": "passkey@example.com", "password": "correct horse battery staple"}, ""))
	if login["stage"] != "password" {
		t.Fatalf("stage after password = %v", login["stage"])
	}

	pending := env.decode(env.do(jar, http.MethodGet, "/api/session", nil, ""))
	pendingCSRF := pending["csrf_token"].(string)

	assertionBegin := env.decode(env.do(jar, http.MethodPost, "/api/auth/passkey/login/begin", nil, pendingCSRF))
	assertionOptions, ok := assertionBegin["options"].(map[string]any)
	if !ok {
		t.Fatalf("missing assertion options: %v", assertionBegin)
	}
	challenge, _ := assertionOptions["challenge"].(string)

	authenticator.signCount = 1
	assertionData := authenticator.authenticatorData(t, options.RelyingParty, 0x05 /* UP|UV */, false)
	clientData := clientDataJSON(t, "webauthn.get", challenge, origin)
	clientDataHash := sha256.Sum256(clientData)
	signedData := append(append([]byte{}, assertionData...), clientDataHash[:]...)
	digest := sha256.Sum256(signedData)

	signature, err := ecdsa.SignASN1(rand.Reader, authenticator.key, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	verified := env.do(jar, http.MethodPost, "/api/auth/passkey/login/finish", map[string]any{
		"id":    b64url(authenticator.credentialID),
		"rawId": b64url(authenticator.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64url(clientData),
			"authenticatorData": b64url(assertionData),
			"signature":         b64url(signature),
			"userHandle":        b64url(authenticator.userHandle),
		},
	}, pendingCSRF)
	if verified.status != http.StatusOK {
		t.Fatalf("assertion status = %d (%s)", verified.status, verified.body)
	}
	if stage := env.decode(verified)["stage"]; stage != "full" {
		t.Fatalf("stage after assertion = %v", stage)
	}

	final := env.decode(env.do(jar, http.MethodGet, "/api/session", nil, ""))
	if final["authenticated"] != true {
		t.Fatalf("expected an authenticated session, got %v", final)
	}
}

// TestPasskeyRegistrationRejectsWrongOrigin ensures the relying party origin is
// enforced, so a ceremony minted for another site cannot be replayed here.
func TestPasskeyRegistrationRejectsWrongOrigin(t *testing.T) {
	env := newTestEnv(t)

	inviteToken := env.createInvite("origin@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	begin := env.decode(env.do(env.client, http.MethodPost, "/api/auth/passkey/register/begin",
		map[string]any{"name": "Wrong Origin"}, csrf))
	options := parseCreationOptions(t, begin)

	authenticator := newVirtualAuthenticator(t)
	attestation, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authenticator.authenticatorData(t, options.RelyingParty, 0x45, true),
	})
	if err != nil {
		t.Fatalf("encode attestation object: %v", err)
	}

	registration := env.do(env.client, http.MethodPost, "/api/auth/passkey/register/finish", map[string]any{
		"id":    b64url(authenticator.credentialID),
		"rawId": b64url(authenticator.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64url(clientDataJSON(t, "webauthn.create", options.Challenge, "https://evil.example")),
			"attestationObject": b64url(attestation),
		},
	}, csrf)
	if registration.status == http.StatusOK {
		t.Fatalf("expected the wrong origin to be rejected, got %s", registration.body)
	}
}

// TestTOTPLoginRateLimit blocks repeated password guessing.
func TestTOTPLoginRateLimit(t *testing.T) {
	env := newTestEnv(t)

	inviteToken := env.createInvite("limited@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	code, err := totp.GenerateCode(setup["secret"].(string), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm", map[string]string{"code": code}, csrf)

	jar := newTestClient(t)
	for attempt := range 5 {
		result := env.do(jar, http.MethodPost, "/api/auth/login",
			map[string]string{"email": "limited@example.com", "password": "wrong password"}, "")
		if result.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt+1, result.status)
		}
	}

	blocked := env.do(jar, http.MethodPost, "/api/auth/login",
		map[string]string{"email": "limited@example.com", "password": "correct horse battery staple"}, "")
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want 429", blocked.status)
	}
}
