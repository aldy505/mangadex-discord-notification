package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type auditEntryResponse struct {
	ID      int64           `json:"id"`
	At      int64           `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Target  *string         `json:"target"`
	Details json.RawMessage `json:"details"`
	IP      *string         `json:"ip"`
}

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			offset = parsed
		}
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	entries, total, err := s.store.ListAudit(r.Context(), limit, offset)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "failed to list audit entries", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to load audit log")

		return
	}

	response := make([]auditEntryResponse, 0, len(entries))
	for _, entry := range entries {
		item := auditEntryResponse{
			ID:      entry.ID,
			At:      entry.At,
			Actor:   entry.Actor,
			Action:  entry.Action,
			Details: json.RawMessage("null"),
		}
		if entry.Target != "" {
			target := entry.Target
			item.Target = &target
		}
		if entry.Details != "" {
			item.Details = json.RawMessage(entry.Details)
		}
		if entry.IP != "" {
			ip := entry.IP
			item.IP = &ip
		}
		response = append(response, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": response,
		"total":   total,
	})
}
