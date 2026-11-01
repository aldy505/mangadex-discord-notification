# Mangadex Discord Notification

Notifies Discord webhooks when new chapters are released on MangaDex, or when an
RSS/Atom feed publishes a new item.

Subscriptions, webhook targets, admins, and an audit trail of every change are
managed through a small admin UI. Everything is stored in SQLite; webhook URLs
live only in the configuration file.

## Features

- MangaDex chapter notifications per manga, optionally filtered by translated
  language.
- Plain RSS/Atom feed notifications (parsed with `gofeed`, HTML converted to
  Discord Markdown).
- Multiple Discord webhook targets, defined in the config file and only
  selectable from a dropdown in the UI.
- One polling goroutine per subscription, with its own interval and its own
  "last seen" watermark, so a slow feed cannot starve the others.
- Admin accounts invited via a single-use link that expires after 24 hours.
- Every admin signs in with username + password **and** a second factor: a
  passkey (recommended) or a TOTP authenticator app. Both can be registered.
- Audit log of logins, invites, factor changes, and subscription changes.
- Vue admin SPA embedded in the binary; no separate web server.

## Quick start

```yaml
services:
  mangadex-discord-notification:
    image: ghcr.io/aldy505/mangadex-discord-notification:latest
    restart: unless-stopped
    environment:
      LISTEN_ADDR: 0.0.0.0:8080
      LOG_LEVEL: info
      # SENTRY_DSN: https://...
    volumes:
      - ./config.json:/app/config.json:ro
      - mangadex-data:/app/data
    ports:
      - "127.0.0.1:8080:8080"

volumes:
  mangadex-data:
```

The container runs as a non-root user (uid 100). A named volume is writable out
of the box; if you bind-mount a host directory instead, make it owned by that
uid (`chown -R 100:101 ./data`).

Copy `config.example.json` to `config.json` and fill in `public_base_url` and at
least one webhook.

### Invite the first admin

There is no default account. Create an invite from the command line:

```bash
docker compose run --rm mangadex-discord-notification \
  invite create -email admin@example.com
```

It prints a link such as `https://manga.example.com/invite/<token>`. The link is
single use and expires in 24 hours. Opening it asks for a password, then forces
the setup of a passkey (recommended) or an authenticator app before the account
becomes usable. Further admins are invited from the *Admins* page of the UI and
are shown the same kind of link.

```bash
# other invite commands
mangadex-discord-notification invite list
mangadex-discord-notification invite revoke -id <invite-id>
```

## Configuration

```json
{
  "listen_addr": "127.0.0.1:8080",
  "public_base_url": "https://manga.example.com",
  "database_path": "/app/data/app.db",
  "poll_interval": "15m",
  "session_idle_timeout": "12h",
  "session_absolute_timeout": "168h",
  "trust_proxy_headers": true,
  "webhooks": [
    { "name": "general", "url": "https://discord.com/api/webhooks/.../...", "default": true },
    { "name": "manga", "url": "https://discord.com/api/webhooks/.../..." }
  ]
}
```

| Field | Description |
| --- | --- |
| `listen_addr` | Address the HTTP server binds to. Defaults to `127.0.0.1:8080`. |
| `public_base_url` | **Required.** Canonical origin used for invite links, cookie security, and WebAuthn. Must be the exact origin users visit. |
| `database_path` | SQLite file. Defaults to `./data/app.db`. |
| `poll_interval` | Default interval for new subscriptions. Defaults to `15m`, minimum `30s`. |
| `session_idle_timeout` | Session inactivity limit. Defaults to `12h`. |
| `session_absolute_timeout` | Hard session lifetime. Defaults to `168h`. |
| `trust_proxy_headers` | Honour `X-Forwarded-For` / `X-Real-Ip` from the reverse proxy. Defaults to `true`; disable it if the service is reachable without a proxy. |
| `webhooks` | Named Discord webhook targets. Exactly one may set `"default": true`; the first entry is the default otherwise. |

Environment overrides: `CONFIG_FILE_PATH`, `LISTEN_ADDR`, `PUBLIC_BASE_URL`,
`DATABASE_PATH`, `POLL_INTERVAL`, `SESSION_IDLE_TIMEOUT`,
`SESSION_ABSOLUTE_TIMEOUT`, `TRUST_PROXY_HEADERS`, `LOG_LEVEL`, `SENTRY_DSN`.

### Reverse proxy notes

The service is meant to sit behind Nginx, Caddy, or Traefik and speak plain
HTTP. Terminate TLS at the proxy and forward to `listen_addr`.

Passkeys require a **domain name**, not an IP address, and they only work over
HTTPS (or `localhost`). `public_base_url` must therefore be the real public
origin, for example `https://manga.example.com` — the WebAuthn relying-party ID
is derived from its hostname. With an IP address the service still starts and
TOTP works normally; passkey endpoints answer `503` and the UI hides the
passkey option, with a warning in the logs.

## Behaviour

- A new subscription's first poll only records a watermark; existing chapters or
  feed items are not announced. Later polls deliver everything newer than that
  watermark, oldest first.
- MangaDex polls walk up to five pages of the chapter feed so a long gap between
  polls does not lose chapters; a chapter that fails to deliver is skipped and
  recorded in the subscription's `last_error`.
- Delivered messages are truncated to Discord's 2000 character limit.
- Feed URLs are fetched exactly as entered. There is no private-network guard,
  so treat admin accounts as trusted.
- Webhook URLs never reach the browser; the UI only receives the webhook name
  and a masked URL.

## Development

```bash
# backend (needs the embedded UI to build)
npm --prefix web ci
npm --prefix web run build

go run . serve -config config.json
go test ./...

# UI hot reload, proxies /api to 127.0.0.1:8080
npm --prefix web run dev
```

The Go binary embeds `web/dist`. `web/dist/.gitkeep` keeps `go:embed` happy
before the first UI build.

## License

[MIT](./LICENSE)
