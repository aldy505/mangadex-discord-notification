package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultConfigPath is used when neither the -config flag nor CONFIG_FILE_PATH
// is provided.
const DefaultConfigPath = "config.json"

const (
	DefaultListenAddr       = "127.0.0.1:8080"
	DefaultDatabasePath     = "./data/app.db"
	DefaultPollInterval     = 15 * time.Minute
	DefaultSessionIdle      = 12 * time.Hour
	DefaultSessionAbsolute  = 7 * 24 * time.Hour
	MinConfigPollInterval   = 30 * time.Second
	AppName                 = "Mangadex Discord Notification"
	MaxSubscriptionInterval = 7 * 24 * time.Hour
	MinSubscriptionInterval = 60 * time.Second

	// InviteTTL is how long an invitation link stays valid.
	InviteTTL = 24 * time.Hour
)

// Webhook is one Discord webhook an admin may target. The URL never leaves the
// server: the UI only ever receives the name.
type Webhook struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Default bool   `json:"default"`
}

// Host returns the webhook's host, for display in the UI.
func (w Webhook) Host() string {
	parsed, err := url.Parse(w.URL)
	if err != nil {
		return ""
	}

	return parsed.Host
}

// MaskedURL is a display-safe rendering of the webhook URL. The path holds the
// webhook token, so it must never be shown verbatim.
func (w Webhook) MaskedURL() string {
	parsed, err := url.Parse(w.URL)
	if err != nil {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host + "/…"
}

type Config struct {
	ListenAddr             string    `json:"listen_addr"`
	PublicBaseURL          string    `json:"public_base_url"`
	DatabasePath           string    `json:"database_path"`
	PollInterval           string    `json:"poll_interval"`
	SessionIdleTimeout     string    `json:"session_idle_timeout"`
	SessionAbsoluteTimeout string    `json:"session_absolute_timeout"`
	TrustProxyHeaders      *bool     `json:"trust_proxy_headers"`
	Webhooks               []Webhook `json:"webhooks"`

	// Derived values, populated by Load.
	Poll            time.Duration `json:"-"`
	SessionIdle     time.Duration `json:"-"`
	SessionAbsolute time.Duration `json:"-"`
	BaseURL         *url.URL      `json:"-"`
	TrustProxy      bool          `json:"-"`
	Path            string        `json:"-"`
}

// Load reads the configuration file, applies environment overrides, fills in
// defaults, and validates the result.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()

	cfg := Config{
		ListenAddr:             DefaultListenAddr,
		DatabasePath:           DefaultDatabasePath,
		PollInterval:           DefaultPollInterval.String(),
		SessionIdleTimeout:     DefaultSessionIdle.String(),
		SessionAbsoluteTimeout: DefaultSessionAbsolute.String(),
		Path:                   path,
	}
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config file %q: %w", path, err)
	}

	applyEnv(&cfg)
	cfg.Webhooks = normalizeWebhooks(cfg.Webhooks)

	if err := cfg.resolve(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyEnv(cfg *Config) {
	if v, ok := lookupEnv("LISTEN_ADDR"); ok {
		cfg.ListenAddr = v
	}
	if v, ok := lookupEnv("PUBLIC_BASE_URL"); ok {
		cfg.PublicBaseURL = v
	}
	if v, ok := lookupEnv("DATABASE_PATH"); ok {
		cfg.DatabasePath = v
	}
	if v, ok := lookupEnv("POLL_INTERVAL"); ok {
		cfg.PollInterval = v
	}
	if v, ok := lookupEnv("SESSION_IDLE_TIMEOUT"); ok {
		cfg.SessionIdleTimeout = v
	}
	if v, ok := lookupEnv("SESSION_ABSOLUTE_TIMEOUT"); ok {
		cfg.SessionAbsoluteTimeout = v
	}
	if v, ok := lookupEnv("TRUST_PROXY_HEADERS"); ok {
		trust := parseBool(v)
		cfg.TrustProxyHeaders = &trust
	}
}

func lookupEnv(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)

	return v, v != ""
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeWebhooks(webhooks []Webhook) []Webhook {
	normalized := make([]Webhook, 0, len(webhooks))
	for _, webhook := range webhooks {
		webhook.Name = strings.TrimSpace(webhook.Name)
		webhook.URL = strings.TrimSpace(webhook.URL)
		normalized = append(normalized, webhook)
	}

	return normalized
}

func (c *Config) resolve() error {
	if c.ListenAddr == "" {
		c.ListenAddr = DefaultListenAddr
	}
	if c.DatabasePath == "" {
		c.DatabasePath = DefaultDatabasePath
	}
	c.TrustProxy = c.TrustProxyHeaders == nil || *c.TrustProxyHeaders

	var err error
	c.Poll, err = parseDuration("poll_interval", c.PollInterval, DefaultPollInterval)
	if err != nil {
		return err
	}
	c.SessionIdle, err = parseDuration("session_idle_timeout", c.SessionIdleTimeout, DefaultSessionIdle)
	if err != nil {
		return err
	}
	c.SessionAbsolute, err = parseDuration("session_absolute_timeout", c.SessionAbsoluteTimeout, DefaultSessionAbsolute)
	if err != nil {
		return err
	}

	if c.PublicBaseURL != "" {
		c.BaseURL, err = url.Parse(c.PublicBaseURL)
		if err != nil {
			return fmt.Errorf("public_base_url: %w", err)
		}
	}

	return nil
}

func parseDuration(field, raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero", field)
	}

	return duration, nil
}

func (c *Config) validate() error {
	if c.BaseURL == nil || c.BaseURL.Host == "" {
		return errors.New("public_base_url is required, e.g. https://manga.example.com")
	}
	if c.BaseURL.Scheme != "http" && c.BaseURL.Scheme != "https" {
		return fmt.Errorf("public_base_url: unsupported scheme %q", c.BaseURL.Scheme)
	}
	if c.Poll < MinConfigPollInterval {
		return fmt.Errorf("poll_interval: must be at least %s", MinConfigPollInterval)
	}
	if c.SessionIdle > c.SessionAbsolute {
		return errors.New("session_idle_timeout must not exceed session_absolute_timeout")
	}
	if len(c.Webhooks) == 0 {
		return errors.New("webhooks: at least one Discord webhook is required")
	}

	seen := make(map[string]struct{}, len(c.Webhooks))
	defaults := 0
	for i, webhook := range c.Webhooks {
		if webhook.Name == "" {
			return fmt.Errorf("webhooks[%d].name is required", i)
		}
		if _, ok := seen[webhook.Name]; ok {
			return fmt.Errorf("webhooks[%d].name %q is duplicated", i, webhook.Name)
		}
		seen[webhook.Name] = struct{}{}

		parsed, err := url.Parse(webhook.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("webhooks[%d].url must be an absolute http(s) URL", i)
		}

		if webhook.Default {
			defaults++
		}
	}
	if defaults > 1 {
		return errors.New("webhooks: at most one webhook may be marked as default")
	}

	return nil
}

// DefaultWebhook returns the webhook new subscriptions default to.
func (c *Config) DefaultWebhook() string {
	for _, webhook := range c.Webhooks {
		if webhook.Default {
			return webhook.Name
		}
	}
	if len(c.Webhooks) > 0 {
		return c.Webhooks[0].Name
	}

	return ""
}

// WebhookByName looks up a webhook by its configured name.
func (c *Config) WebhookByName(name string) (Webhook, bool) {
	for _, webhook := range c.Webhooks {
		if webhook.Name == name {
			return webhook, true
		}
	}

	return Webhook{}, false
}

// BaseURLString returns the configured public base URL without a trailing
// slash.
func (c *Config) BaseURLString() string {
	if c.BaseURL == nil {
		return ""
	}

	return strings.TrimRight(c.BaseURL.String(), "/")
}
