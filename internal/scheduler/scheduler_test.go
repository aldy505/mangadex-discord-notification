package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/store"
)

type recordingWebhook struct {
	mu       sync.Mutex
	requests []string
	server   *httptest.Server
}

func newRecordingWebhook(t *testing.T) *recordingWebhook {
	t.Helper()

	hook := &recordingWebhook{}
	hook.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hook.mu.Lock()
		hook.requests = append(hook.requests, string(body))
		hook.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(hook.server.Close)

	return hook
}

func (h *recordingWebhook) bodies() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.requests...)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestManager(t *testing.T, webhookURL string, mangadexBaseURL string) *Manager {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	baseURL, err := url.Parse("http://localhost")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	cfg := &config.Config{
		BaseURL: baseURL,
		Poll:    time.Minute,
		Webhooks: []config.Webhook{
			{Name: "main", URL: webhookURL, Default: true},
		},
	}

	var mangadexClient *mangadex.Client
	if mangadexBaseURL != "" {
		mangadexClient, err = mangadex.NewClient(mangadexBaseURL, nil)
		if err != nil {
			t.Fatalf("new mangadex client: %v", err)
		}
	}

	return NewManager(cfg, st, mangadexClient, nil, discardLogger())
}

func TestPollFeedSkipsExistingItemsOnFirstRun(t *testing.T) {
	hook := newRecordingWebhook(t)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>
			<item><title>Old</title><link>https://example.com/old</link>
			<description>old</description><pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate></item>
			</channel></rss>`)
	}))
	t.Cleanup(feedServer.Close)

	manager := newTestManager(t, hook.server.URL, "")
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindRSS,
		Label:           "Feed",
		Source:          feedServer.URL,
		Webhook:         "main",
		IntervalSeconds: 60,
		Enabled:         true,
	}

	watermark, err := manager.pollFeed(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollFeed: %v", err)
	}
	if watermark == nil {
		t.Fatal("expected the watermark to be initialised")
	}
	if got := hook.bodies(); len(got) != 0 {
		t.Fatalf("expected no deliveries on the first run, got %d", len(got))
	}
}

func TestPollFeedDeliversOnlyNewItems(t *testing.T) {
	hook := newRecordingWebhook(t)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>
			<item><title>Old</title><link>https://example.com/old</link>
			<description>&lt;p&gt;old&lt;/p&gt;</description><pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate></item>
			<item><title>New</title><link>https://example.com/new</link>
			<description>&lt;p&gt;fresh &lt;b&gt;news&lt;/b&gt;&lt;/p&gt;</description><pubDate>Tue, 03 Jan 2023 15:04:05 GMT</pubDate></item>
			</channel></rss>`)
	}))
	t.Cleanup(feedServer.Close)

	manager := newTestManager(t, hook.server.URL, "")

	oldItem := time.Date(2023, 1, 2, 15, 4, 5, 0, time.UTC).UnixMilli()
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindRSS,
		Label:           "Feed",
		Source:          feedServer.URL,
		Webhook:         "main",
		IntervalSeconds: 60,
		Enabled:         true,
		Watermark:       &oldItem,
	}

	watermark, err := manager.pollFeed(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollFeed: %v", err)
	}

	bodies := hook.bodies()
	if len(bodies) != 1 {
		t.Fatalf("deliveries = %d, want 1: %v", len(bodies), bodies)
	}
	if !contains(bodies[0], "New") || !contains(bodies[0], "fresh **news**") {
		t.Errorf("unexpected payload: %s", bodies[0])
	}
	if contains(bodies[0], "Old") {
		t.Errorf("delivered an item at or before the watermark: %s", bodies[0])
	}

	newItem := time.Date(2023, 1, 3, 15, 4, 5, 0, time.UTC).UnixMilli()
	if watermark == nil || *watermark != newItem {
		t.Fatalf("watermark = %v, want %d", watermark, newItem)
	}
}

func TestPollFeedRecordsWebhookRemovedFromConfig(t *testing.T) {
	manager := newTestManager(t, "http://127.0.0.1:1/unused", "")

	oldItem := time.Now().Add(-time.Hour).UnixMilli()
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindRSS,
		Label:           "Feed",
		Source:          "http://127.0.0.1:2/feed",
		Webhook:         "deleted",
		IntervalSeconds: 60,
		Enabled:         true,
		Watermark:       &oldItem,
	}

	if _, err := manager.pollFeed(context.Background(), subscription, discardLogger()); err == nil {
		t.Fatal("expected an error for a webhook that is no longer configured")
	}
}

func TestPollMangaDeliversNewChapters(t *testing.T) {
	hook := newRecordingWebhook(t)

	createdAt := time.Now().UTC().Truncate(time.Second)
	mangaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":"ok","response":"collection","limit":100,"offset":0,"total":1,"data":[
			{"id":"chapter-1","type":"chapter","attributes":{"chapter":"7","title":"The Reveal",
			"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}}]}`,
			createdAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano),
			createdAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano))
	}))
	t.Cleanup(mangaServer.Close)

	manager := newTestManager(t, hook.server.URL, mangaServer.URL)

	watermark := createdAt.Add(-time.Hour).UnixMilli()
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindMangadex,
		Label:           "One Piece",
		Source:          "manga-id",
		Webhook:         "main",
		Languages:       "en",
		IntervalSeconds: 60,
		Enabled:         true,
		Watermark:       &watermark,
	}

	newWatermark, err := manager.pollManga(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollManga: %v", err)
	}

	bodies := hook.bodies()
	if len(bodies) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(bodies))
	}
	if !contains(bodies[0], "One Piece - The Reveal") || !contains(bodies[0], "Ch. 7") {
		t.Errorf("unexpected payload: %s", bodies[0])
	}
	if !contains(bodies[0], "https://mangadex.org/chapter/chapter-1") {
		t.Errorf("missing chapter URL: %s", bodies[0])
	}

	if newWatermark == nil || *newWatermark != createdAt.UnixMilli() {
		t.Fatalf("watermark = %v, want %d", newWatermark, createdAt.UnixMilli())
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}

	return -1
}
