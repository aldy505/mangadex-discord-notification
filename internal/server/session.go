package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/store"
)

type requestState struct {
	Session *store.Session
	Admin   *store.Admin
}

type requestStateKey struct{}

func stateFrom(r *http.Request) *requestState {
	state, _ := r.Context().Value(requestStateKey{}).(*requestState)

	return state
}

// withSession resolves the session cookie, enforces CSRF on state changing
// requests, and makes the session available to the handler.
func (s *Server) withSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := s.resolveSession(w, r)
		if state != nil {
			r = r.WithContext(context.WithValue(r.Context(), requestStateKey{}, state))
		}

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if state != nil && !constantTimeEqual(r.Header.Get("X-CSRF-Token"), state.Session.CSRFToken) {
				writeError(w, http.StatusForbidden, "invalid or missing CSRF token")

				return
			}
		}

		next(w, r)
	}
}

// requireFull rejects requests from sessions that have not completed both
// factors.
func (s *Server) requireFull(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := stateFrom(r)
		if state == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")

			return
		}
		if state.Session.Stage != store.StageFull {
			writeError(w, http.StatusUnauthorized, "second factor required")

			return
		}

		next(w, r)
	}
}

// requireStage allows a handler to run only for the given session stages.
func (s *Server) requireStage(next http.HandlerFunc, stages ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := stateFrom(r)
		if state == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")

			return
		}
		for _, stage := range stages {
			if state.Session.Stage == stage {
				next(w, r)

				return
			}
		}

		writeError(w, http.StatusUnauthorized, "this step is not available for the current session")
	}
}

func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) *requestState {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	ctx := r.Context()
	session, err := s.store.GetSession(ctx, auth.HashToken(cookie.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.ErrorContext(ctx, "failed to load session", slogError(err))
		}
		s.clearSessionCookie(w)

		return nil
	}

	now := s.now().UnixMilli()
	if now >= session.ExpiresAt || now-session.LastSeenAt >= s.cfg.SessionIdle.Milliseconds() {
		_ = s.store.DeleteSession(ctx, session.ID)
		s.clearSessionCookie(w)

		return nil
	}

	admin, err := s.store.GetAdminByID(ctx, session.AdminID)
	if err != nil {
		_ = s.store.DeleteSession(ctx, session.ID)
		s.clearSessionCookie(w)

		return nil
	}

	// Throttle last_seen writes: one per minute per session is enough.
	if now-session.LastSeenAt > time.Minute.Milliseconds() {
		if err := s.store.TouchSession(ctx, session.ID, now); err != nil {
			s.logger.WarnContext(ctx, "failed to touch session", slogError(err))
		}
		session.LastSeenAt = now
	}

	return &requestState{Session: session, Admin: admin}
}

// startSession issues a new session for an admin and sets the cookie.
func (s *Server) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, admin *store.Admin, stage string) (*store.Session, string, error) {
	rawToken, tokenHash, err := auth.NewToken(32)
	if err != nil {
		return nil, "", err
	}

	csrfToken, _, err := auth.NewToken(32)
	if err != nil {
		return nil, "", err
	}

	now := s.now().UnixMilli()
	session := store.Session{
		ID:         tokenHash,
		AdminID:    admin.ID,
		Stage:      stage,
		CSRFToken:  csrfToken,
		CreatedAt:  now,
		ExpiresAt:  now + s.cfg.SessionAbsolute.Milliseconds(),
		LastSeenAt: now,
		UserAgent:  s.userAgent(r),
		IP:         s.clientIP(r),
	}

	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, "", err
	}

	s.setSessionCookie(w, rawToken)

	return &session, csrfToken, nil
}

// promoteSession moves the current session to a new stage and rotates its CSRF
// token.
func (s *Server) promoteSession(ctx context.Context, state *requestState, stage string) (string, error) {
	csrfToken, _, err := auth.NewToken(32)
	if err != nil {
		return "", err
	}

	if err := s.store.PromoteSession(ctx, state.Session.ID, stage, csrfToken); err != nil {
		return "", err
	}

	state.Session.Stage = stage
	state.Session.CSRFToken = csrfToken

	return csrfToken, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.cfg.SessionAbsolute.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// loadCredentials returns the admin's registered WebAuthn credentials.
func (s *Server) loadCredentials(ctx context.Context, adminID string) ([]webauthn.Credential, error) {
	stored, err := s.store.ListWebAuthnCredentials(ctx, adminID)
	if err != nil {
		return nil, err
	}

	credentials := make([]webauthn.Credential, 0, len(stored))
	for _, credential := range stored {
		parsed, err := auth.UnmarshalCredential(credential.Data)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to parse stored passkey",
				slogError(err), slogString("credential_id", credential.ID))

			continue
		}
		credentials = append(credentials, parsed)
	}

	return credentials, nil
}

func credentialID(credential *webauthn.Credential) string {
	return base64.RawURLEncoding.EncodeToString(credential.ID)
}
