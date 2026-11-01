package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/store"
)

type adminResponse struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	CreatedAt    int64  `json:"created_at"`
	TOTP         bool   `json:"totp"`
	PasskeyCount int    `json:"passkey_count"`
}

type inviteResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	State     string `json:"state"`
	URL       string `json:"url,omitempty"`
}

func (s *Server) handleAdminsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	admins, err := s.store.ListAdmins(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list admins", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to list admins")

		return
	}

	response := make([]adminResponse, 0, len(admins))
	for _, admin := range admins {
		hasTOTP, passkeys, err := s.store.AdminFactors(ctx, admin.ID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to load factors", slogError(err))
			writeError(w, http.StatusInternalServerError, "failed to list admins")

			return
		}
		response = append(response, adminResponse{
			ID:           admin.ID,
			Email:        admin.Email,
			CreatedAt:    admin.CreatedAt,
			TOTP:         hasTOTP,
			PasskeyCount: passkeys,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"admins": response})
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := stateFrom(r)
	id := r.PathValue("id")

	if id == state.Admin.ID {
		writeError(w, http.StatusConflict, "you cannot remove your own account")

		return
	}

	admins, err := s.store.CountAdmins(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to count admins", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove admin")

		return
	}
	if admins <= 1 {
		writeError(w, http.StatusConflict, "at least one admin must remain")

		return
	}

	admin, err := s.store.GetAdminByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "admin not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to load admin", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove admin")

		return
	}

	if err := s.store.DeleteAdmin(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "admin not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to delete admin", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove admin")

		return
	}

	s.auditState(r, store.ActionAdminRemoved, id, map[string]any{"email": admin.Email})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInvitesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	invites, err := s.store.ListInvites(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list invites", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to list invites")

		return
	}

	now := store.Now()
	response := make([]inviteResponse, 0, len(invites))
	for _, invite := range invites {
		response = append(response, inviteResponse{
			ID:        invite.ID,
			Email:     invite.Email,
			CreatedAt: invite.CreatedAt,
			ExpiresAt: invite.ExpiresAt,
			State:     invite.State(now),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"invites": response})
}

func (s *Server) handleInviteCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		writeFieldError(w, http.StatusBadRequest, "invalid email address", map[string]string{
			"email": "enter a valid email address",
		})

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	if _, err := s.store.GetAdminByEmail(ctx, email); err == nil {
		writeFieldError(w, http.StatusConflict, "an admin with this email already exists", map[string]string{
			"email": "an admin with this email already exists",
		})

		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.logger.ErrorContext(ctx, "failed to look up admin", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to create invite")

		return
	}

	rawToken, tokenHash, err := auth.NewToken(32)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to generate invite token", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to create invite")

		return
	}

	now := store.Now()
	invite := store.Invite{
		ID:        uuid.NewString(),
		TokenHash: tokenHash,
		Email:     email,
		CreatedBy: state.Admin.ID,
		CreatedAt: now,
		ExpiresAt: now + config.InviteTTL.Milliseconds(),
	}

	if err := s.store.CreateInvite(ctx, invite); err != nil {
		s.logger.ErrorContext(ctx, "failed to create invite", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to create invite")

		return
	}

	s.auditState(r, store.ActionInviteCreated, invite.ID, map[string]any{"email": email})

	writeJSON(w, http.StatusCreated, inviteResponse{
		ID:        invite.ID,
		Email:     invite.Email,
		CreatedAt: invite.CreatedAt,
		ExpiresAt: invite.ExpiresAt,
		State:     store.InviteStatePending,
		URL:       fmt.Sprintf("%s/invite/%s", s.cfg.BaseURLString(), rawToken),
	})
}

func (s *Server) handleInviteRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := s.store.RevokeInvite(ctx, id, store.Now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invite not found or already used")

			return
		}
		s.logger.ErrorContext(ctx, "failed to revoke invite", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to revoke invite")

		return
	}

	s.auditState(r, store.ActionInviteRevoked, id, nil)
	w.WriteHeader(http.StatusNoContent)
}
