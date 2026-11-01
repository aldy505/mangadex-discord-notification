CREATE TABLE admins (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    email      TEXT NOT NULL,
    created_by TEXT REFERENCES admins(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER,
    revoked_at INTEGER
);

CREATE INDEX idx_invites_email ON invites (email);

CREATE TABLE sessions (
    id                        TEXT PRIMARY KEY,
    admin_id                  TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    stage                     TEXT NOT NULL,
    csrf_token                TEXT NOT NULL,
    created_at                INTEGER NOT NULL,
    expires_at                INTEGER NOT NULL,
    last_seen_at              INTEGER NOT NULL,
    user_agent                TEXT NOT NULL DEFAULT '',
    ip                        TEXT NOT NULL DEFAULT '',
    totp_pending_secret       TEXT,
    totp_pending_created_at   INTEGER,
    webauthn_register_session TEXT,
    webauthn_login_session    TEXT,
    passkey_pending_name      TEXT
);

CREATE INDEX idx_sessions_admin ON sessions (admin_id);

CREATE TABLE totp_credentials (
    admin_id     TEXT PRIMARY KEY REFERENCES admins(id) ON DELETE CASCADE,
    secret       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);

CREATE TABLE webauthn_credentials (
    id           TEXT PRIMARY KEY,
    admin_id     TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    data         TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);

CREATE INDEX idx_webauthn_credentials_admin ON webauthn_credentials (admin_id);

CREATE TABLE subscriptions (
    id               TEXT PRIMARY KEY,
    kind             TEXT NOT NULL CHECK (kind IN ('mangadex', 'rss')),
    label            TEXT NOT NULL,
    source           TEXT NOT NULL,
    webhook          TEXT NOT NULL,
    languages        TEXT NOT NULL DEFAULT '',
    interval_seconds INTEGER NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 1,
    watermark        INTEGER,
    last_checked_at  INTEGER,
    last_error       TEXT,
    created_by       TEXT REFERENCES admins(id) ON DELETE SET NULL,
    created_at       INTEGER NOT NULL
);

CREATE UNIQUE INDEX idx_subscriptions_unique ON subscriptions (kind, source, webhook);
CREATE INDEX idx_subscriptions_enabled ON subscriptions (enabled);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    at         INTEGER NOT NULL,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    target     TEXT,
    details    TEXT,
    ip         TEXT,
    user_agent TEXT
);

CREATE INDEX idx_audit_log_at ON audit_log (at DESC);
