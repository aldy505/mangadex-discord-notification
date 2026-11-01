package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pquerna/otp/totp"

	"mangadex-discord-notification/internal/auth"
	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/scheduler"
	"mangadex-discord-notification/internal/store"
)

type testEnv struct {
	t       *testing.T
	server  *httptest.Server
	client  *http.Client
	store   *store.Store
	manager *scheduler.Manager
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	return newTestEnvConfigured(t, "http://localhost", "")
}

func newTestEnvWithMangadex(t *testing.T, mangadexBaseURL string) *testEnv {
	t.Helper()

	return newTestEnvConfigured(t, "http://localhost", mangadexBaseURL)
}

func newTestEnvConfigured(t *testing.T, baseURL, mangadexBaseURL string) *testEnv {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	cfg := &config.Config{
		BaseURL:         parsedBaseURL,
		ListenAddr:      "127.0.0.1:0",
		DatabasePath:    "test.db",
		Poll:            60 * time.Second,
		SessionIdle:     12 * time.Hour,
		SessionAbsolute: 24 * time.Hour,
		TrustProxy:      true,
		Webhooks: []config.Webhook{
			{Name: "main", URL: "http://127.0.0.1:1/hook", Default: true},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var mangadexClient *mangadex.Client
	if mangadexBaseURL != "" {
		mangadexClient, err = mangadex.NewClient(mangadexBaseURL, nil)
		if err != nil {
			t.Fatalf("mangadex client: %v", err)
		}
	}

	manager := scheduler.NewManager(cfg, st, mangadexClient, nil, logger)

	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		manager.Shutdown()
		manager.Wait()
	})

	ui := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>admin</title>")},
	}

	api, err := New(cfg, st, manager, mangadexClient, ui, logger)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return &testEnv{
		t:       t,
		server:  server,
		client:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
		store:   st,
		manager: manager,
	}
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

type apiResult struct {
	status int
	body   []byte
}

func (e *testEnv) do(client *http.Client, method, path string, payload any, csrf string) apiResult {
	e.t.Helper()

	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			e.t.Fatalf("marshal payload: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}

	response, err := client.Do(request)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	return apiResult{status: response.StatusCode, body: body}
}

func (e *testEnv) decode(result apiResult) map[string]any {
	e.t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(result.body, &decoded); err != nil {
		e.t.Fatalf("decode %s: %v", string(result.body), err)
	}

	return decoded
}

func (e *testEnv) createInvite(email string) string {
	e.t.Helper()

	rawToken, tokenHash, err := auth.NewToken(32)
	if err != nil {
		e.t.Fatalf("new token: %v", err)
	}

	now := store.Now()
	if err := e.store.CreateInvite(context.Background(), store.Invite{
		ID:        "invite-1",
		TokenHash: tokenHash,
		Email:     email,
		CreatedAt: now,
		ExpiresAt: now + 24*time.Hour.Milliseconds(),
	}); err != nil {
		e.t.Fatalf("create invite: %v", err)
	}

	return rawToken
}

func TestAdminOnboardingAndSubscriptionManagement(t *testing.T) {
	env := newTestEnv(t)

	session := env.decode(env.do(env.client, http.MethodGet, "/api/session", nil, ""))
	if session["stage"] != "anonymous" {
		t.Fatalf("initial stage = %v", session["stage"])
	}

	inviteToken := env.createInvite("admin@example.com")

	info := env.do(env.client, http.MethodGet, "/api/invites/"+inviteToken, nil, "")
	if info.status != http.StatusOK {
		t.Fatalf("invite info status = %d (%s)", info.status, info.body)
	}

	accept := env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, "")
	if accept.status != http.StatusOK {
		t.Fatalf("accept status = %d (%s)", accept.status, accept.body)
	}
	accepted := env.decode(accept)
	if accepted["stage"] != "enroll" {
		t.Fatalf("stage after accept = %v", accepted["stage"])
	}
	csrf := accepted["csrf_token"].(string)

	// The invite cannot be reused, even from a fresh browser.
	reuseJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	reuse := env.do(
		&http.Client{Jar: reuseJar, Timeout: 10 * time.Second},
		http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "another long password"}, "")
	if reuse.status != http.StatusGone {
		t.Fatalf("reuse status = %d, want 410", reuse.status)
	}

	// A session that has not enrolled a second factor cannot reach admin APIs.
	if blocked := env.do(env.client, http.MethodGet, "/api/subscriptions", nil, ""); blocked.status != http.StatusUnauthorized {
		t.Fatalf("unenrolled access status = %d, want 401", blocked.status)
	}

	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	secret, _ := setup["secret"].(string)
	if secret == "" {
		t.Fatalf("missing TOTP secret: %v", setup)
	}
	if qr, _ := setup["qr_png_data_url"].(string); !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatalf("unexpected QR payload")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}

	confirm := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm",
		map[string]string{"code": code}, csrf))
	if confirm["stage"] != "full" {
		t.Fatalf("stage after confirm = %v", confirm["stage"])
	}
	csrf = confirm["csrf_token"].(string)

	// State changing requests require the CSRF token.
	if missing := env.do(env.client, http.MethodPost, "/api/subscriptions",
		map[string]any{"kind": "rss", "url": "http://example.com/feed.xml"}, ""); missing.status != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missing.status)
	}

	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Site Updates</title>
			<item><title>Post</title><link>https://example.com/post</link><description>hi</description>
			<pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate></item></channel></rss>`)
	}))
	defer feedServer.Close()

	created := env.do(env.client, http.MethodPost, "/api/subscriptions", map[string]any{
		"kind":             "rss",
		"url":              feedServer.URL,
		"webhook":          "main",
		"interval_seconds": 300,
	}, csrf)
	if created.status != http.StatusCreated {
		t.Fatalf("create subscription status = %d (%s)", created.status, created.body)
	}
	subscription := env.decode(created)
	if subscription["label"] != "Site Updates" {
		t.Errorf("label = %v", subscription["label"])
	}
	if subscription["webhook"] != "main" {
		t.Errorf("webhook = %v", subscription["webhook"])
	}

	// Duplicates are rejected.
	duplicate := env.do(env.client, http.MethodPost, "/api/subscriptions", map[string]any{
		"kind": "rss", "url": feedServer.URL, "webhook": "main",
	}, csrf)
	if duplicate.status != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", duplicate.status)
	}

	// An unknown webhook is rejected.
	badWebhook := env.do(env.client, http.MethodPost, "/api/subscriptions", map[string]any{
		"kind": "rss", "url": feedServer.URL + "?x=1", "webhook": "nope",
	}, csrf)
	if badWebhook.status != http.StatusBadRequest {
		t.Fatalf("unknown webhook status = %d, want 400", badWebhook.status)
	}

	list := env.decode(env.do(env.client, http.MethodGet, "/api/subscriptions", nil, ""))
	subscriptions, _ := list["subscriptions"].([]any)
	if len(subscriptions) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(subscriptions))
	}

	id, _ := subscription["id"].(string)
	if deleted := env.do(env.client, http.MethodDelete, "/api/subscriptions/"+id, nil, csrf); deleted.status != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleted.status)
	}

	audit := env.decode(env.do(env.client, http.MethodGet, "/api/audit?limit=50", nil, ""))
	if total, _ := audit["total"].(float64); total < 4 {
		t.Fatalf("audit total = %v, want at least 4", audit["total"])
	}
}

func TestLoginRequiresSecondFactor(t *testing.T) {
	env := newTestEnv(t)

	inviteToken := env.createInvite("admin@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	secret := setup["secret"].(string)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm", map[string]string{"code": code}, csrf)

	// A fresh browser starts from scratch.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	freshClient := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	if wrong := env.do(freshClient, http.MethodPost, "/api/auth/login",
		map[string]string{"email": "admin@example.com", "password": "wrong password"}, ""); wrong.status != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", wrong.status)
	}

	login := env.decode(env.do(freshClient, http.MethodPost, "/api/auth/login",
		map[string]string{"email": "admin@example.com", "password": "correct horse battery staple"}, ""))
	if login["stage"] != "password" {
		t.Fatalf("stage after password = %v", login["stage"])
	}
	factors, _ := login["factors"].(map[string]any)
	if factors["totp"] != true {
		t.Fatalf("factors = %v", factors)
	}

	pending := env.decode(env.do(freshClient, http.MethodGet, "/api/session", nil, ""))
	pendingCSRF := pending["csrf_token"].(string)
	if pending["authenticated"] != false {
		t.Fatal("a pending session must not be authenticated")
	}

	if unauthorised := env.do(freshClient, http.MethodGet, "/api/account", nil, ""); unauthorised.status != http.StatusUnauthorized {
		t.Fatalf("pending session account access = %d, want 401", unauthorised.status)
	}

	code, err = totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}

	verified := env.decode(env.do(freshClient, http.MethodPost, "/api/auth/totp",
		map[string]string{"code": code}, pendingCSRF))
	if verified["stage"] != "full" {
		t.Fatalf("stage after TOTP = %v", verified["stage"])
	}

	session := env.decode(env.do(freshClient, http.MethodGet, "/api/session", nil, ""))
	if session["authenticated"] != true {
		t.Fatalf("expected an authenticated session, got %v", session)
	}
}

func TestUnknownAPIRoutesAndSPAFallback(t *testing.T) {
	env := newTestEnv(t)

	missing := env.do(env.client, http.MethodGet, "/api/definitely-not-a-route", nil, "")
	if missing.status != http.StatusNotFound {
		t.Fatalf("unknown api status = %d, want 404", missing.status)
	}
	var payload map[string]any
	if err := json.Unmarshal(missing.body, &payload); err != nil || payload["error"] == nil {
		t.Fatalf("expected a JSON error, got %s", missing.body)
	}

	spa := env.do(env.client, http.MethodGet, "/invite/some-token", nil, "")
	if spa.status != http.StatusOK || !strings.Contains(string(spa.body), "<title>admin</title>") {
		t.Fatalf("SPA fallback status = %d body = %s", spa.status, spa.body)
	}
}

func TestAdminInvitationsAndGuards(t *testing.T) {
	env := newTestEnv(t)

	inviteToken := env.createInvite("first@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	code, err := totp.GenerateCode(setup["secret"].(string), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	confirm := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm", map[string]string{"code": code}, csrf))
	csrf = confirm["csrf_token"].(string)

	created := env.do(env.client, http.MethodPost, "/api/invites", map[string]string{"email": "Second@Example.com"}, csrf)
	if created.status != http.StatusCreated {
		t.Fatalf("create invite status = %d (%s)", created.status, created.body)
	}
	invite := env.decode(created)
	if invite["email"] != "second@example.com" {
		t.Errorf("email = %v", invite["email"])
	}
	url, _ := invite["url"].(string)
	if !strings.HasPrefix(url, "http://localhost/invite/") {
		t.Errorf("invite url = %q", url)
	}

	if duplicate := env.do(env.client, http.MethodPost, "/api/invites", map[string]string{"email": "first@example.com"}, csrf); duplicate.status != http.StatusConflict {
		t.Errorf("inviting an existing admin status = %d, want 409", duplicate.status)
	}
	if invalid := env.do(env.client, http.MethodPost, "/api/invites", map[string]string{"email": "not-an-email"}, csrf); invalid.status != http.StatusBadRequest {
		t.Errorf("invalid email status = %d, want 400", invalid.status)
	}

	// Removing yourself is refused.
	session := env.decode(env.do(env.client, http.MethodGet, "/api/session", nil, ""))
	adminID := session["admin"].(map[string]any)["id"].(string)
	if self := env.do(env.client, http.MethodDelete, "/api/admins/"+adminID, nil, csrf); self.status != http.StatusConflict {
		t.Errorf("self delete status = %d, want 409", self.status)
	}

	// Removing the only second factor is refused.
	if onlyFactor := env.do(env.client, http.MethodDelete, "/api/account/totp", nil, csrf); onlyFactor.status != http.StatusConflict {
		t.Errorf("removing last factor status = %d, want 409", onlyFactor.status)
	}
}

func TestMangaSubscriptionAndPreview(t *testing.T) {
	mangaID := "a1c7c817-4e59-43b7-9365-09675a149a6f"
	mangaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manga/"+mangaID {
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)

			return
		}
		if got := r.URL.Query().Get("includes[]"); got != "cover_art" {
			t.Errorf("includes = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":"ok","response":"entity","data":{
			"id":%q,"type":"manga","attributes":{
				"title":{"en":"One Piece","ja":"ワンピース"},
				"status":"ongoing","lastChapter":"1120","contentRating":"safe",
				"availableTranslatedLanguages":["en","es"],"year":1997},
			"relationships":[{"id":"cover-1","type":"cover_art","attributes":{"fileName":"cover.png"}}]}}`, mangaID)
	}))
	defer mangaServer.Close()

	env := newTestEnvWithMangadex(t, mangaServer.URL)

	inviteToken := env.createInvite("manga@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	code, err := totp.GenerateCode(setup["secret"].(string), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	csrf = env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm",
		map[string]string{"code": code}, csrf))["csrf_token"].(string)

	// A pasted mangadex.org URL is accepted and resolved.
	preview := env.do(env.client, http.MethodPost, "/api/preview/mangadex",
		map[string]string{"input": "https://mangadex.org/title/" + mangaID + "/one-piece"}, csrf)
	if preview.status != http.StatusOK {
		t.Fatalf("preview status = %d (%s)", preview.status, preview.body)
	}
	previewed := env.decode(preview)
	if previewed["title"] != "One Piece" {
		t.Errorf("title = %v", previewed["title"])
	}
	if previewed["cover_url"] != "https://uploads.mangadex.org/covers/"+mangaID+"/cover.png.512.jpg" {
		t.Errorf("cover url = %v", previewed["cover_url"])
	}

	created := env.do(env.client, http.MethodPost, "/api/subscriptions", map[string]any{
		"kind":             "mangadex",
		"manga_id":         "https://mangadex.org/title/" + mangaID + "/one-piece",
		"webhook":          "main",
		"languages":        []string{"en", "ES"},
		"interval_seconds": 600,
	}, csrf)
	if created.status != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", created.status, created.body)
	}

	subscription := env.decode(created)
	if subscription["source"] != mangaID {
		t.Errorf("source = %v, want the canonical manga ID", subscription["source"])
	}
	if subscription["label"] != "One Piece" {
		t.Errorf("label = %v", subscription["label"])
	}
	languages, _ := subscription["languages"].([]any)
	if len(languages) != 2 || languages[0] != "en" || languages[1] != "es" {
		t.Errorf("languages = %v", languages)
	}

	invalid := env.do(env.client, http.MethodPost, "/api/subscriptions", map[string]any{
		"kind": "mangadex", "manga_id": "not-a-manga", "webhook": "main",
	}, csrf)
	if invalid.status != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", invalid.status)
	}
}

// TestPasskeysDegradeOnIPBaseURL keeps TOTP-only deployments working when the
// configured origin is an IP address, which WebAuthn does not allow.
func TestPasskeysDegradeOnIPBaseURL(t *testing.T) {
	env := newTestEnvConfigured(t, "http://127.0.0.1:8080", "")

	session := env.decode(env.do(env.client, http.MethodGet, "/api/session", nil, ""))
	if session["passkeys_available"] != false {
		t.Fatalf("passkeys_available = %v, want false", session["passkeys_available"])
	}

	inviteToken := env.createInvite("ipadmin@example.com")
	accept := env.decode(env.do(env.client, http.MethodPost, "/api/invites/"+inviteToken+"/accept",
		map[string]string{"password": "correct horse battery staple"}, ""))
	csrf := accept["csrf_token"].(string)

	passkey := env.do(env.client, http.MethodPost, "/api/auth/passkey/register/begin",
		map[string]any{"name": "Nope"}, csrf)
	if passkey.status != http.StatusServiceUnavailable {
		t.Fatalf("passkey begin status = %d, want 503", passkey.status)
	}

	// TOTP remains a complete second factor.
	setup := env.decode(env.do(env.client, http.MethodPost, "/api/auth/totp/setup", map[string]any{}, csrf))
	code, err := totp.GenerateCode(setup["secret"].(string), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	confirm := env.do(env.client, http.MethodPost, "/api/auth/totp/setup/confirm",
		map[string]string{"code": code}, csrf)
	if confirm.status != http.StatusOK {
		t.Fatalf("TOTP confirm status = %d (%s)", confirm.status, confirm.body)
	}
	if stage := env.decode(confirm)["stage"]; stage != "full" {
		t.Fatalf("stage = %v, want full", stage)
	}
}
