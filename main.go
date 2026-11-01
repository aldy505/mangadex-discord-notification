package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/scheduler"
	"mangadex-discord-notification/internal/server"
	"mangadex-discord-notification/internal/store"
	"mangadex-discord-notification/web"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "serve":
		return runServe(args)
	case "invite":
		return runInvite(args)
	case "help", "-h", "--help":
		usage()

		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		usage()

		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Mangadex Discord Notification

Usage:
  mangadex-discord-notification [serve] [-config path]
  mangadex-discord-notification invite create -email admin@example.com [-config path]
  mangadex-discord-notification invite list [-config path]
  mangadex-discord-notification invite revoke -id <invite-id> [-config path]

Environment overrides:
  CONFIG_FILE_PATH, LISTEN_ADDR, PUBLIC_BASE_URL, DATABASE_PATH, POLL_INTERVAL,
  SESSION_IDLE_TIMEOUT, SESSION_ABSOLUTE_TIMEOUT, TRUST_PROXY_HEADERS,
  LOG_LEVEL, SENTRY_DSN
`)
}

func defaultConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("CONFIG_FILE_PATH")); path != "" {
		return path
	}

	return config.DefaultConfigPath
}

func configureLogging(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})))
}

func runServe(args []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "path to the configuration file")
	logLevel := flags.String("log-level", envOr("LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	configureLogging(*logLevel)

	if dsn := os.Getenv("SENTRY_DSN"); dsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, SampleRate: 1.0}); err != nil {
			slog.Error("failed to initialize sentry", slog.String("error", err.Error()))
		} else {
			defer sentry.Flush(2 * time.Second)
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", slog.String("error", err.Error()))

		return 1
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		slog.Error("failed to create database directory", slog.String("error", err.Error()))

		return 1
	}

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open database", slog.String("error", err.Error()))

		return 1
	}
	defer func() {
		if err := st.Close(); err != nil {
			slog.Warn("failed to close database", slog.String("error", err.Error()))
		}
	}()

	mangadexClient, err := mangadex.NewClient(mangadex.APIBaseURL, nil)
	if err != nil {
		slog.Error("failed to create mangadex client", slog.String("error", err.Error()))

		return 1
	}

	ui, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		slog.Error("failed to mount admin UI", slog.String("error", err.Error()))

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	manager := scheduler.NewManager(cfg, st, mangadexClient, nil, slog.Default())
	if err := manager.Start(ctx); err != nil {
		slog.Error("failed to start scheduler", slog.String("error", err.Error()))

		return 1
	}
	defer func() {
		manager.Shutdown()
		manager.Wait()
	}()

	api, err := server.New(cfg, st, manager, mangadexClient, ui, slog.Default())
	if err != nil {
		slog.Error("failed to create server", slog.String("error", err.Error()))

		return 1
	}

	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := st.DeleteStaleSessions(context.Background(), store.Now(), cfg.SessionIdle.Milliseconds()); err != nil {
					slog.Warn("failed to prune sessions", slog.String("error", err.Error()))
				}
			}
		}
	}()

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", slog.String("address", cfg.ListenAddr), slog.String("public_base_url", cfg.BaseURLString()))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			slog.Error("http server stopped", slog.String("error", err.Error()))

			return 1
		}
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("failed to shut down http server", slog.String("error", err.Error()))
	}

	return 0
}

func runInvite(args []string) int {
	if len(args) == 0 {
		usage()

		return 2
	}

	action := args[0]
	flags := flag.NewFlagSet("invite "+action, flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "path to the configuration file")
	// The flag set is parsed per action so unrelated flags do not error out.
	email := ""
	id := ""

	switch action {
	case "create":
		flags.StringVar(&email, "email", "", "email address of the invited admin")
	case "list":
	case "revoke":
		flags.StringVar(&id, "id", "", "invite ID to revoke")
	default:
		fmt.Fprintf(os.Stderr, "unknown invite command %q\n\n", action)
		usage()

		return 2
	}

	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}

	configureLogging(envOr("LOG_LEVEL", "info"))

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)

		return 1
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create database directory: %v\n", err)

		return 1
	}

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)

		return 1
	}
	defer st.Close()

	ctx := context.Background()

	switch action {
	case "create":
		if email = strings.ToLower(strings.TrimSpace(email)); email == "" {
			fmt.Fprintln(os.Stderr, "-email is required")

			return 2
		}

		if _, err := st.GetAdminByEmail(ctx, email); err == nil {
			fmt.Fprintf(os.Stderr, "an admin with email %s already exists\n", email)

			return 1
		} else if !errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "failed to look up admin: %v\n", err)

			return 1
		}

		rawToken, tokenHash, err := auth.NewToken(32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate invite token: %v\n", err)

			return 1
		}

		now := store.Now()
		invite := store.Invite{
			ID:        uuid.NewString(),
			TokenHash: tokenHash,
			Email:     email,
			CreatedAt: now,
			ExpiresAt: now + config.InviteTTL.Milliseconds(),
		}

		if err := st.CreateInvite(ctx, invite); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create invite: %v\n", err)

			return 1
		}

		details, _ := json.Marshal(map[string]string{"email": email})
		if err := st.InsertAudit(ctx, store.AuditEntry{
			Actor:   "cli",
			Action:  store.ActionInviteCreated,
			Target:  invite.ID,
			Details: string(details),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write audit entry: %v\n", err)
		}

		fmt.Printf("Invite for %s (expires in 24h):\n%s/invite/%s\n", email, cfg.BaseURLString(), rawToken)
		fmt.Printf("\ninvite id: %s\n", invite.ID)

	case "list":
		invites, err := st.ListInvites(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to list invites: %v\n", err)

			return 1
		}

		if len(invites) == 0 {
			fmt.Println("no invites")

			return 0
		}

		now := store.Now()
		for _, invite := range invites {
			fmt.Printf("%s\t%s\t%s\texpires %s\n",
				invite.ID,
				invite.State(now),
				invite.Email,
				time.UnixMilli(invite.ExpiresAt).UTC().Format(time.RFC3339),
			)
		}

	case "revoke":
		if strings.TrimSpace(id) == "" {
			fmt.Fprintln(os.Stderr, "-id is required")

			return 2
		}

		if err := st.RevokeInvite(ctx, id, store.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to revoke invite: %v\n", err)

			return 1
		}

		if err := st.InsertAudit(ctx, store.AuditEntry{
			Actor:  "cli",
			Action: store.ActionInviteRevoked,
			Target: id,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write audit entry: %v\n", err)
		}

		fmt.Println("invite revoked")
	}

	return 0
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}
