package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/scheduler"
	"mangadex-discord-notification/internal/store"
)

const (
	sessionCookieName = "mdn_session"
	maxRequestBody    = 1 << 20
)

// Server serves the JSON API and the embedded Vue admin UI.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	sched    *scheduler.Manager
	webauthn *webauthn.WebAuthn
	// passkeyProblem explains why passkeys are disabled, if they are.
	passkeyProblem string
	mangadex       *mangadex.Client
	logger         *slog.Logger
	ui             fs.FS
	limiter        *loginLimiter
	secure         bool
	handler        http.Handler

	dummyHash string
	now       func() time.Time
}

func New(
	cfg *config.Config,
	st *store.Store,
	sched *scheduler.Manager,
	mangadexClient *mangadex.Client,
	ui fs.FS,
	logger *slog.Logger,
) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	relyingParty, passkeyErr := auth.NewWebAuthn(cfg.BaseURL, config.AppName)
	if passkeyErr != nil {
		// Passkeys are spec-impossible without a domain name, but TOTP alone is a
		// complete second factor, so degrade instead of refusing to boot.
		logger.Warn("passkeys disabled; public_base_url must be a domain name",
			slog.String("error", passkeyErr.Error()))
	}

	dummyHash, err := auth.HashPassword("timing-equalisation-placeholder")
	if err != nil {
		return nil, fmt.Errorf("prepare password verifier: %w", err)
	}

	server := &Server{
		cfg:            cfg,
		store:          st,
		sched:          sched,
		webauthn:       relyingParty,
		passkeyProblem: errorString(passkeyErr),
		mangadex:       mangadexClient,
		logger:         logger,
		ui:             ui,
		limiter:        newLoginLimiter(5, 15*time.Minute),
		secure:         strings.EqualFold(cfg.BaseURL.Scheme, "https"),
		dummyHash:      dummyHash,
		now:            time.Now,
	}
	server.handler = server.routes()

	return server, nil
}

// requirePasskeys reports whether the ceremony can run, writing a 503 when it
// cannot.
func (s *Server) requirePasskeys(w http.ResponseWriter) bool {
	if s.webauthn == nil {
		writeError(w, http.StatusServiceUnavailable, "passkeys are unavailable: "+s.passkeyProblem)

		return false
	}

	return true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints.
	mux.HandleFunc("GET /api/healthz", s.handleHealth)
	mux.HandleFunc("GET /api/session", s.withSession(s.handleSession))
	mux.HandleFunc("POST /api/auth/login", s.withSession(s.handleLogin))
	mux.HandleFunc("POST /api/auth/totp", s.withSession(s.requireStage(s.handleTOTPVerify, store.StagePassword)))
	mux.HandleFunc("POST /api/auth/passkey/login/begin", s.withSession(s.requireStage(s.handlePasskeyLoginBegin, store.StagePassword)))
	mux.HandleFunc("POST /api/auth/passkey/login/finish", s.withSession(s.requireStage(s.handlePasskeyLoginFinish, store.StagePassword)))
	mux.HandleFunc("POST /api/auth/logout", s.withSession(s.handleLogout))
	mux.HandleFunc("GET /api/invites/{token}", s.withSession(s.handleInviteInfo))
	mux.HandleFunc("POST /api/invites/{token}/accept", s.withSession(s.handleInviteAccept))

	// Enrollment (allowed while a factor is still missing, and for adding a
	// second factor to an already authenticated account).
	mux.HandleFunc("POST /api/auth/totp/setup", s.withSession(s.requireStage(s.handleTOTPSetup, store.StageEnroll, store.StageFull)))
	mux.HandleFunc("POST /api/auth/totp/setup/confirm", s.withSession(s.requireStage(s.handleTOTPSetupConfirm, store.StageEnroll, store.StageFull)))
	mux.HandleFunc("POST /api/auth/passkey/register/begin", s.withSession(s.requireStage(s.handlePasskeyRegisterBegin, store.StageEnroll, store.StageFull)))
	mux.HandleFunc("POST /api/auth/passkey/register/finish", s.withSession(s.requireStage(s.handlePasskeyRegisterFinish, store.StageEnroll, store.StageFull)))

	// Authenticated endpoints.
	mux.HandleFunc("GET /api/account", s.withSession(s.requireFull(s.handleAccount)))
	mux.HandleFunc("PUT /api/account/password", s.withSession(s.requireFull(s.handlePasswordChange)))
	mux.HandleFunc("DELETE /api/account/totp", s.withSession(s.requireFull(s.handleTOTPRemove)))
	mux.HandleFunc("DELETE /api/account/passkeys/{id}", s.withSession(s.requireFull(s.handlePasskeyRemove)))

	mux.HandleFunc("GET /api/admins", s.withSession(s.requireFull(s.handleAdminsList)))
	mux.HandleFunc("DELETE /api/admins/{id}", s.withSession(s.requireFull(s.handleAdminDelete)))
	mux.HandleFunc("GET /api/invites", s.withSession(s.requireFull(s.handleInvitesList)))
	mux.HandleFunc("POST /api/invites", s.withSession(s.requireFull(s.handleInviteCreate)))
	mux.HandleFunc("DELETE /api/invites/{id}", s.withSession(s.requireFull(s.handleInviteRevoke)))

	mux.HandleFunc("GET /api/webhooks", s.withSession(s.requireFull(s.handleWebhooks)))
	mux.HandleFunc("GET /api/subscriptions", s.withSession(s.requireFull(s.handleSubscriptionsList)))
	mux.HandleFunc("POST /api/subscriptions", s.withSession(s.requireFull(s.handleSubscriptionCreate)))
	mux.HandleFunc("DELETE /api/subscriptions/{id}", s.withSession(s.requireFull(s.handleSubscriptionDelete)))
	mux.HandleFunc("POST /api/preview/mangadex", s.withSession(s.requireFull(s.handleMangaPreview)))
	mux.HandleFunc("POST /api/preview/rss", s.withSession(s.requireFull(s.handleFeedPreview)))

	mux.HandleFunc("GET /api/audit", s.withSession(s.requireFull(s.handleAuditList)))

	// Unknown API paths must not fall through to the SPA.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	// Everything else is the SPA.
	mux.HandleFunc("/", s.handleSPA)

	return s.securityHeaders(mux)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Permissions-Policy", "publickey-credentials-get=(self), publickey-credentials-create=(self)")
		header.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if s.secure {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	if s.ui == nil {
		http.Error(w, "UI is not built", http.StatusServiceUnavailable)

		return
	}

	requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requested != "" && !strings.Contains(requested, "..") {
		if data, err := fs.ReadFile(s.ui, requested); err == nil {
			contentType := mime.TypeByExtension(path.Ext(requested))
			if contentType == "" {
				contentType = http.DetectContentType(data)
			}
			w.Header().Set("Content-Type", contentType)
			if strings.HasPrefix(requested, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}

			return
		}
	}

	index, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		http.Error(w, "UI is not built", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodGet {
		_, _ = w.Write(index)
	}
}

// --- JSON helpers ---

type errorResponse struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", slog.String("error", err.Error()))
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeFieldError(w http.ResponseWriter, status int, message string, fields map[string]string) {
	writeJSON(w, status, errorResponse{Error: message, Fields: fields})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	return nil
}

// --- request IP / client metadata ---

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" && net.ParseIP(realIP) != nil {
			return realIP
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func (s *Server) userAgent(r *http.Request) string {
	return truncateString(r.Header.Get("User-Agent"), 256)
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit]
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
