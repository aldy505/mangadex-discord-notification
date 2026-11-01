package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/store"
)

const (
	totpSetupTTL       = 15 * time.Minute
	maxPasskeyNameSize = 64
)

type adminSummary struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt int64  `json:"created_at"`
}

func toAdminSummary(admin *store.Admin) *adminSummary {
	if admin == nil {
		return nil
	}

	return &adminSummary{ID: admin.ID, Email: admin.Email, CreatedAt: admin.CreatedAt}
}

type factorsResponse struct {
	TOTP     bool `json:"totp"`
	Passkeys int  `json:"passkeys"`
}

type sessionResponse struct {
	Stage             string          `json:"stage"`
	Authenticated     bool            `json:"authenticated"`
	Admin             *adminSummary   `json:"admin"`
	CSRFToken         *string         `json:"csrf_token"`
	Factors           factorsResponse `json:"factors"`
	PublicBaseURL     string          `json:"public_base_url"`
	PasskeysAvailable bool            `json:"passkeys_available"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	response := sessionResponse{
		Stage:             "anonymous",
		PublicBaseURL:     s.cfg.BaseURLString(),
		PasskeysAvailable: s.webauthn != nil,
	}

	state := stateFrom(r)
	if state == nil {
		writeJSON(w, http.StatusOK, response)

		return
	}

	hasTOTP, passkeys, err := s.store.AdminFactors(r.Context(), state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "failed to load factors", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to load account")

		return
	}

	csrf := state.Session.CSRFToken
	response.Stage = state.Session.Stage
	response.Authenticated = state.Session.Stage == store.StageFull
	response.Admin = toAdminSummary(state.Admin)
	response.CSRFToken = &csrf
	response.Factors = factorsResponse{TOTP: hasTOTP, Passkeys: passkeys}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")

		return
	}

	ctx := r.Context()
	limitKey := s.clientIP(r) + "|" + email
	if blocked, retryAfter := s.limiter.blocked(limitKey); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")

		return
	}

	admin, err := s.store.GetAdminByEmail(ctx, email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.ErrorContext(ctx, "failed to look up admin", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to sign in")

		return
	}

	encodedHash := s.dummyHash
	if admin != nil {
		encodedHash = admin.PasswordHash
	}

	valid, verifyErr := auth.VerifyPassword(encodedHash, body.Password)
	if verifyErr != nil || !valid || admin == nil {
		s.limiter.failure(limitKey)
		s.audit(ctx, r, email, store.ActionLoginFailed, "", map[string]any{"reason": "invalid credentials"})
		writeError(w, http.StatusUnauthorized, "invalid credentials")

		return
	}
	s.limiter.reset(limitKey)

	hasTOTP, passkeys, err := s.store.AdminFactors(ctx, admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load factors", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to sign in")

		return
	}

	stage := store.StagePassword
	if !hasTOTP && passkeys == 0 {
		// The admin never finished enrolling a second factor.
		stage = store.StageEnroll
	}

	if _, _, err := s.startSession(ctx, w, r, admin, stage); err != nil {
		s.logger.ErrorContext(ctx, "failed to create session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to sign in")

		return
	}

	s.audit(ctx, r, admin.Email, store.ActionLoginSucceeded, admin.ID, map[string]any{"stage": stage})

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":   stage,
		"factors": factorsResponse{TOTP: hasTOTP, Passkeys: passkeys},
	})
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	code := strings.TrimSpace(body.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	credential, err := s.store.GetTOTP(ctx, state.Admin.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "no authenticator app is registered for this account")

			return
		}
		s.logger.ErrorContext(ctx, "failed to load TOTP credential", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to verify code")

		return
	}

	if !auth.ValidateTOTP(credential.Secret, code) {
		s.audit(ctx, r, state.Admin.Email, store.ActionLoginFailed, state.Admin.ID, map[string]any{"reason": "invalid TOTP code"})
		writeError(w, http.StatusUnauthorized, "invalid code")

		return
	}

	if err := s.store.TouchTOTP(ctx, state.Admin.ID, store.Now()); err != nil {
		s.logger.WarnContext(ctx, "failed to record TOTP use", slogError(err))
	}

	csrfToken, err := s.promoteSession(ctx, state, store.StageFull)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to complete session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to complete sign in")

		return
	}

	s.audit(ctx, r, state.Admin.Email, store.ActionLoginSucceeded, state.Admin.ID, map[string]any{
		"stage":  store.StageFull,
		"factor": "totp",
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":      store.StageFull,
		"csrf_token": csrfToken,
	})
}

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeys(w) {
		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	credentials, err := s.loadCredentials(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load passkeys", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey sign in")

		return
	}
	if len(credentials) == 0 {
		writeError(w, http.StatusBadRequest, "no passkey is registered for this account")

		return
	}

	user := &auth.WebAuthnUser{ID: state.Admin.ID, Email: state.Admin.Email, Credentials: credentials}
	assertion, sessionData, err := s.webauthn.BeginLogin(user)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to begin passkey login", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey sign in")

		return
	}

	encoded, err := auth.MarshalSessionData(sessionData)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey ceremony", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey sign in")

		return
	}
	if err := s.store.SetWebAuthnLoginSession(ctx, state.Session.ID, encoded); err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey ceremony", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey sign in")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"options": assertion.Response})
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeys(w) {
		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	if state.Session.WebAuthnLoginSession == "" {
		writeError(w, http.StatusBadRequest, "no passkey ceremony in progress")

		return
	}

	sessionData, err := auth.UnmarshalSessionData(state.Session.WebAuthnLoginSession)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to restore passkey ceremony", slogError(err))
		writeError(w, http.StatusBadRequest, "no passkey ceremony in progress")

		return
	}

	credentials, err := s.loadCredentials(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load passkeys", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to verify passkey")

		return
	}

	user := &auth.WebAuthnUser{ID: state.Admin.ID, Email: state.Admin.Email, Credentials: credentials}
	credential, err := s.webauthn.FinishLogin(user, sessionData, r)
	if err != nil {
		s.audit(ctx, r, state.Admin.Email, store.ActionLoginFailed, state.Admin.ID, map[string]any{"reason": "passkey verification failed"})
		writeError(w, http.StatusUnauthorized, "passkey verification failed")

		return
	}

	encoded, err := auth.MarshalCredential(credential)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to serialize passkey", slogError(err))
	} else if err := s.store.UpdateWebAuthnCredential(ctx, credentialID(credential), encoded, store.Now()); err != nil {
		s.logger.WarnContext(ctx, "failed to update passkey counter", slogError(err))
	}
	_ = s.store.ClearWebAuthnLoginSession(ctx, state.Session.ID)

	csrfToken, err := s.promoteSession(ctx, state, store.StageFull)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to complete session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to complete sign in")

		return
	}

	s.audit(ctx, r, state.Admin.Email, store.ActionLoginSucceeded, state.Admin.ID, map[string]any{
		"stage":  store.StageFull,
		"factor": "passkey",
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":      store.StageFull,
		"csrf_token": csrfToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if state := stateFrom(r); state != nil {
		if err := s.store.DeleteSession(ctx, state.Session.ID); err != nil {
			s.logger.WarnContext(ctx, "failed to delete session", slogError(err))
		}
		s.audit(ctx, r, state.Admin.Email, store.ActionLogout, state.Admin.ID, nil)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInviteInfo(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if len(token) < 16 {
		writeError(w, http.StatusNotFound, "invite not found")

		return
	}

	invite, err := s.store.GetInviteByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invite not found")

			return
		}
		s.logger.ErrorContext(r.Context(), "failed to load invite", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to load invite")

		return
	}

	if invite.State(store.Now()) != store.InviteStatePending {
		writeError(w, http.StatusNotFound, "invite is no longer valid")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"email":      invite.Email,
		"expires_at": invite.ExpiresAt,
		"valid":      true,
	})
}

func (s *Server) handleInviteAccept(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	token := r.PathValue("token")
	if len(token) < 16 {
		writeError(w, http.StatusNotFound, "invite not found")

		return
	}

	ctx := r.Context()
	invite, err := s.store.GetInviteByTokenHash(ctx, auth.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invite not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to load invite", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to accept invite")

		return
	}

	if invite.State(store.Now()) != store.InviteStatePending {
		writeError(w, http.StatusGone, "invite is no longer valid")

		return
	}

	if err := auth.ValidatePassword(body.Password); err != nil {
		writeFieldError(w, http.StatusBadRequest, "password does not meet the requirements", map[string]string{
			"password": err.Error(),
		})

		return
	}

	consumed, err := s.store.ConsumeInvite(ctx, invite.ID, store.Now())
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to consume invite", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to accept invite")

		return
	}
	if !consumed {
		writeError(w, http.StatusGone, "invite is no longer valid")

		return
	}

	passwordHash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to hash password", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to accept invite")

		return
	}

	admin, err := s.store.CreateAdmin(ctx, uuid.NewString(), invite.Email, passwordHash)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "an account with this email already exists")

			return
		}
		s.logger.ErrorContext(ctx, "failed to create admin", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to accept invite")

		return
	}

	_, csrfToken, err := s.startSession(ctx, w, r, admin, store.StageEnroll)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to accept invite")

		return
	}

	s.audit(ctx, r, admin.Email, store.ActionInviteAccepted, invite.ID, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":      store.StageEnroll,
		"csrf_token": csrfToken,
	})
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := stateFrom(r)

	setup, err := auth.GenerateTOTP(config.AppName, state.Admin.Email)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to generate TOTP secret", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start authenticator setup")

		return
	}

	if err := s.store.SetTOTPPending(ctx, state.Session.ID, setup.Secret); err != nil {
		s.logger.ErrorContext(ctx, "failed to store TOTP secret", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start authenticator setup")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"secret":          setup.Secret,
		"otpauth_url":     setup.OTPAuthURL,
		"qr_png_data_url": setup.QRPNGDataURL,
	})
}

func (s *Server) handleTOTPSetupConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	if state.Session.TOTPPendingSecret == "" {
		writeError(w, http.StatusBadRequest, "no authenticator setup in progress")

		return
	}

	if store.Now()-state.Session.TOTPPendingCreatedAt > totpSetupTTL.Milliseconds() {
		writeError(w, http.StatusBadRequest, "authenticator setup expired, start again")

		return
	}

	code := strings.TrimSpace(body.Code)
	if !auth.ValidateTOTP(state.Session.TOTPPendingSecret, code) {
		writeError(w, http.StatusBadRequest, "invalid code")

		return
	}

	if err := s.store.UpsertTOTP(ctx, state.Admin.ID, state.Session.TOTPPendingSecret); err != nil {
		s.logger.ErrorContext(ctx, "failed to store TOTP credential", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish authenticator setup")

		return
	}
	_ = s.store.ClearTOTPPending(ctx, state.Session.ID)

	csrfToken, err := s.promoteSession(ctx, state, store.StageFull)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to complete session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish authenticator setup")

		return
	}

	s.audit(ctx, r, state.Admin.Email, store.ActionTOTPEnrolled, state.Admin.ID, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":      store.StageFull,
		"csrf_token": csrfToken,
	})
}

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeys(w) {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > maxPasskeyNameSize {
		name = name[:maxPasskeyNameSize]
	}

	credentials, err := s.loadCredentials(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load passkeys", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey setup")

		return
	}

	user := &auth.WebAuthnUser{ID: state.Admin.ID, Email: state.Admin.Email, Credentials: credentials}
	creation, sessionData, err := s.webauthn.BeginRegistration(user)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to begin passkey registration", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey setup")

		return
	}

	encoded, err := auth.MarshalSessionData(sessionData)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey ceremony", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey setup")

		return
	}

	if err := s.store.SetWebAuthnRegisterSession(ctx, state.Session.ID, encoded); err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey ceremony", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey setup")

		return
	}
	if err := s.store.SetPasskeyPendingName(ctx, state.Session.ID, name); err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey name", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to start passkey setup")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"options": creation.Response})
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !s.requirePasskeys(w) {
		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	if state.Session.WebAuthnRegisterSession == "" {
		writeError(w, http.StatusBadRequest, "no passkey ceremony in progress")

		return
	}

	sessionData, err := auth.UnmarshalSessionData(state.Session.WebAuthnRegisterSession)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to restore passkey ceremony", slogError(err))
		writeError(w, http.StatusBadRequest, "no passkey ceremony in progress")

		return
	}

	credentials, err := s.loadCredentials(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load passkeys", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish passkey setup")

		return
	}

	user := &auth.WebAuthnUser{ID: state.Admin.ID, Email: state.Admin.Email, Credentials: credentials}
	credential, err := s.webauthn.FinishRegistration(user, sessionData, r)
	if err != nil {
		s.logger.WarnContext(ctx, "passkey registration failed", slogError(err))
		writeError(w, http.StatusBadRequest, "passkey registration failed: "+err.Error())

		return
	}

	encoded, err := auth.MarshalCredential(credential)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to serialize passkey", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish passkey setup")

		return
	}

	name := state.Session.PasskeyPendingName
	if name == "" {
		name = "Passkey"
	}

	if err := s.store.CreateWebAuthnCredential(ctx, store.WebAuthnCredential{
		ID:      credentialID(credential),
		AdminID: state.Admin.ID,
		Name:    name,
		Data:    encoded,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to store passkey", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish passkey setup")

		return
	}
	_ = s.store.ClearWebAuthnRegisterSession(ctx, state.Session.ID)

	csrfToken, err := s.promoteSession(ctx, state, store.StageFull)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to complete session", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to finish passkey setup")

		return
	}

	s.audit(ctx, r, state.Admin.Email, store.ActionPasskeyEnrolled, credentialID(credential), map[string]any{"name": name})

	writeJSON(w, http.StatusOK, map[string]any{
		"stage":      store.StageFull,
		"csrf_token": csrfToken,
	})
}

// audit records an action performed by an identified actor.
func (s *Server) audit(ctx context.Context, r *http.Request, actor, action, target string, details map[string]any) {
	entry := store.AuditEntry{
		At:        store.Now(),
		Actor:     actor,
		Action:    action,
		Target:    target,
		IP:        s.clientIP(r),
		UserAgent: s.userAgent(r),
	}
	if details != nil {
		encoded, err := json.Marshal(details)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to encode audit details", slogError(err))
		} else {
			entry.Details = string(encoded)
		}
	}

	if err := s.store.InsertAudit(ctx, entry); err != nil {
		s.logger.ErrorContext(ctx, "failed to write audit entry", slogError(err))
	}
}

// auditState records an action performed by the current admin.
func (s *Server) auditState(r *http.Request, action, target string, details map[string]any) {
	state := stateFrom(r)
	actor := "system"
	if state != nil && state.Admin != nil {
		actor = state.Admin.Email
	}
	s.audit(r.Context(), r, actor, action, target, details)
}

// decodeOptionalJSON decodes a JSON body, tolerating an empty body.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}
