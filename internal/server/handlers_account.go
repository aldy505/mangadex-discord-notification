package server

import (
	"errors"
	"net/http"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/store"
)

type passkeyResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := stateFrom(r)

	hasTOTP, _, err := s.store.AdminFactors(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load factors", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to load account")

		return
	}

	credentials, err := s.store.ListWebAuthnCredentials(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load passkeys", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to load account")

		return
	}

	passkeys := make([]passkeyResponse, 0, len(credentials))
	for _, credential := range credentials {
		passkeys = append(passkeys, passkeyResponse{
			ID:         credential.ID,
			Name:       credential.Name,
			CreatedAt:  credential.CreatedAt,
			LastUsedAt: credential.LastUsedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"admin":    toAdminSummary(state.Admin),
		"totp":     hasTOTP,
		"passkeys": passkeys,
	})
}

func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	valid, err := auth.VerifyPassword(state.Admin.PasswordHash, body.CurrentPassword)
	if err != nil || !valid {
		writeFieldError(w, http.StatusBadRequest, "current password is incorrect", map[string]string{
			"current_password": "current password is incorrect",
		})

		return
	}

	if err := auth.ValidatePassword(body.NewPassword); err != nil {
		writeFieldError(w, http.StatusBadRequest, "password does not meet the requirements", map[string]string{
			"new_password": err.Error(),
		})

		return
	}

	passwordHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to hash password", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	if err := s.store.UpdateAdminPassword(ctx, state.Admin.ID, passwordHash); err != nil {
		s.logger.ErrorContext(ctx, "failed to update password", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to change password")

		return
	}

	// A password change invalidates every other session.
	if err := s.store.DeleteOtherSessions(ctx, state.Admin.ID, state.Session.ID); err != nil {
		s.logger.WarnContext(ctx, "failed to revoke other sessions", slogError(err))
	}

	s.auditState(r, store.ActionPasswordChanged, state.Admin.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTOTPRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := stateFrom(r)

	hasTOTP, passkeys, err := s.store.AdminFactors(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load factors", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove authenticator app")

		return
	}
	if !hasTOTP {
		writeError(w, http.StatusNotFound, "no authenticator app is registered")

		return
	}
	if passkeys == 0 {
		writeError(w, http.StatusConflict, "set up a passkey before removing your only second factor")

		return
	}

	if err := s.store.DeleteTOTP(ctx, state.Admin.ID); err != nil {
		s.logger.ErrorContext(ctx, "failed to delete TOTP credential", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove authenticator app")

		return
	}

	s.auditState(r, store.ActionTOTPRemoved, state.Admin.ID, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePasskeyRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := stateFrom(r)
	id := r.PathValue("id")

	hasTOTP, passkeys, err := s.store.AdminFactors(ctx, state.Admin.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to load factors", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove passkey")

		return
	}
	if !hasTOTP && passkeys <= 1 {
		writeError(w, http.StatusConflict, "set up another factor before removing your only second factor")

		return
	}

	if err := s.store.DeleteWebAuthnCredential(ctx, state.Admin.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "passkey not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to delete passkey", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove passkey")

		return
	}

	s.auditState(r, store.ActionPasskeyRemoved, id, map[string]any{"id": truncateString(id, 64)})
	w.WriteHeader(http.StatusNoContent)
}
