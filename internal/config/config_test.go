package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"public_base_url": "https://manga.example.com",
		"webhooks": [{"name": "main", "url": "https://discord.com/api/webhooks/1/token"}]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ListenAddr != DefaultListenAddr {
		t.Errorf("listen addr = %q, want %q", cfg.ListenAddr, DefaultListenAddr)
	}
	if cfg.Poll != DefaultPollInterval {
		t.Errorf("poll interval = %s, want %s", cfg.Poll, DefaultPollInterval)
	}
	if !cfg.TrustProxy {
		t.Error("trust proxy should default to true")
	}
	if got := cfg.DefaultWebhook(); got != "main" {
		t.Errorf("default webhook = %q, want main", got)
	}
	if got := cfg.BaseURLString(); got != "https://manga.example.com" {
		t.Errorf("base URL = %q", got)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	path := writeConfig(t, `{
		"listen_addr": "0.0.0.0:8080",
		"public_base_url": "https://manga.example.com",
		"webhooks": [{"name": "main", "url": "https://discord.com/api/webhooks/1/token"}]
	}`)

	t.Setenv("LISTEN_ADDR", "127.0.0.1:9999")
	t.Setenv("DATABASE_PATH", "/tmp/other.db")
	t.Setenv("POLL_INTERVAL", "45m")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:9999" {
		t.Errorf("listen addr = %q", cfg.ListenAddr)
	}
	if cfg.DatabasePath != "/tmp/other.db" {
		t.Errorf("database path = %q", cfg.DatabasePath)
	}
	if cfg.Poll != 45*time.Minute {
		t.Errorf("poll interval = %s", cfg.Poll)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	webhook := `{"name": "main", "url": "https://discord.com/api/webhooks/1/token"}`

	cases := map[string]string{
		"missing base url": `{"webhooks": [` + webhook + `]}`,
		"no webhooks":      `{"public_base_url": "https://manga.example.com"}`,
		"duplicate names":  `{"public_base_url": "https://manga.example.com", "webhooks": [` + webhook + `,` + webhook + `]}`,
		"relative url": `{"public_base_url": "https://manga.example.com",
			"webhooks": [{"name": "main", "url": "/api/webhooks/1"}]}`,
		"two defaults": `{"public_base_url": "https://manga.example.com",
			"webhooks": [{"name": "a", "url": "https://x.test/1", "default": true},
			             {"name": "b", "url": "https://x.test/2", "default": true}]}`,
		"idle exceeds absolute": `{"public_base_url": "https://manga.example.com",
			"session_idle_timeout": "48h", "session_absolute_timeout": "24h",
			"webhooks": [` + webhook + `]}`,
		"poll too small": `{"public_base_url": "https://manga.example.com", "poll_interval": "1s",
			"webhooks": [` + webhook + `]}`,
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, contents)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestWebhookMasking(t *testing.T) {
	webhook := Webhook{Name: "main", URL: "https://discord.com/api/webhooks/123/secret-token"}

	if got := webhook.Host(); got != "discord.com" {
		t.Errorf("host = %q", got)
	}
	if got := webhook.MaskedURL(); got != "https://discord.com/…" {
		t.Errorf("masked url = %q", got)
	}
}
