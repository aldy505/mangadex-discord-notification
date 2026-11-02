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
	status   int
	server   *httptest.Server
}

func newRecordingWebhook(t *testing.T) *recordingWebhook {
	t.Helper()

	hook := &recordingWebhook{status: http.StatusNoContent}
	hook.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		hook.mu.Lock()
		// Attempts are recorded even when they fail, so tests can count retries.
		hook.requests = append(hook.requests, string(body))
		status := hook.status
		hook.mu.Unlock()

		w.WriteHeader(status)
	}))
	t.Cleanup(hook.server.Close)

	return hook
}

// respond sets the status code every following delivery attempt answers with.
func (h *recordingWebhook) respond(status int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.status = status
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

func TestPollFeedFirstRunDeliversNewestItem(t *testing.T) {
	hook := newRecordingWebhook(t)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>
			<item><title>Old</title><link>https://example.com/old</link>
			<description>old</description><pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate></item>
			<item><title>New</title><link>https://example.com/new</link>
			<description>new</description><pubDate>Tue, 03 Jan 2023 15:04:05 GMT</pubDate></item>
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

	bodies := hook.bodies()
	if len(bodies) != 1 {
		t.Fatalf("deliveries on the first run = %d, want 1 baseline item: %v", len(bodies), bodies)
	}
	if !contains(bodies[0], "New") {
		t.Errorf("baseline did not announce the newest item: %s", bodies[0])
	}
	if contains(bodies[0], "Old") {
		t.Errorf("baseline announced an older item: %s", bodies[0])
	}

	newItem := time.Date(2023, 1, 3, 15, 4, 5, 0, time.UTC).UnixMilli()
	if watermark == nil || *watermark != newItem {
		t.Fatalf("watermark = %v, want %d", watermark, newItem)
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

func TestPollMangaFirstRunDeliversNewestChapter(t *testing.T) {
	hook := newRecordingWebhook(t)

	older := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)

	// The real API answers newest first.
	mangaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":"ok","response":"collection","limit":100,"offset":0,"total":2,"data":[
			{"id":"chapter-2","type":"chapter","attributes":{"chapter":"2","title":"Second",
			"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}},
			{"id":"chapter-1","type":"chapter","attributes":{"chapter":"1","title":"First",
			"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}}]}`,
			newer.Format(time.RFC3339Nano), newer.Format(time.RFC3339Nano), newer.Format(time.RFC3339Nano), newer.Format(time.RFC3339Nano),
			older.Format(time.RFC3339Nano), older.Format(time.RFC3339Nano), older.Format(time.RFC3339Nano), older.Format(time.RFC3339Nano))
	}))
	t.Cleanup(mangaServer.Close)

	manager := newTestManager(t, hook.server.URL, mangaServer.URL)
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindMangadex,
		Label:           "One Piece",
		Source:          "manga-id",
		Webhook:         "main",
		Languages:       "en",
		IntervalSeconds: 60,
		Enabled:         true,
	}

	watermark, err := manager.pollManga(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollManga: %v", err)
	}

	bodies := hook.bodies()
	if len(bodies) != 1 {
		t.Fatalf("deliveries on the first run = %d, want 1 baseline chapter: %v", len(bodies), bodies)
	}
	if !contains(bodies[0], "https://mangadex.org/chapter/chapter-2") {
		t.Errorf("baseline did not announce the newest chapter: %s", bodies[0])
	}
	if contains(bodies[0], "chapter-1") {
		t.Errorf("baseline announced an older chapter: %s", bodies[0])
	}

	if watermark == nil || *watermark != newer.UnixMilli() {
		t.Fatalf("watermark = %v, want %d", watermark, newer.UnixMilli())
	}
}

func TestPollMangaStopsAtFirstFailureAndRetries(t *testing.T) {
	hook := newRecordingWebhook(t)

	t1 := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	// The real API answers newest first.
	mangaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":"ok","response":"collection","limit":100,"offset":0,"total":2,"data":[
			{"id":"chapter-2","type":"chapter","attributes":{"chapter":"2","title":"Second",
			"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}},
			{"id":"chapter-1","type":"chapter","attributes":{"chapter":"1","title":"First",
			"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}}]}`,
			t2.Format(time.RFC3339Nano), t2.Format(time.RFC3339Nano), t2.Format(time.RFC3339Nano), t2.Format(time.RFC3339Nano),
			t1.Format(time.RFC3339Nano), t1.Format(time.RFC3339Nano), t1.Format(time.RFC3339Nano), t1.Format(time.RFC3339Nano))
	}))
	t.Cleanup(mangaServer.Close)

	manager := newTestManager(t, hook.server.URL, mangaServer.URL)

	watermark := t1.Add(-time.Hour).UnixMilli()
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

	hook.respond(http.StatusInternalServerError)

	failed, err := manager.pollManga(context.Background(), subscription, discardLogger())
	if err == nil || !contains(err.Error(), "deliver chapter chapter-1") {
		t.Fatalf("pollManga error = %v, want it to name chapter-1", err)
	}
	if failed == nil || *failed != watermark {
		t.Fatalf("watermark after failure = %v, want it unchanged at %d", failed, watermark)
	}
	if got := len(hook.bodies()); got != 1 {
		t.Fatalf("delivery attempts after failure = %d, want 1", got)
	}

	hook.respond(http.StatusNoContent)

	recovered, err := manager.pollManga(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollManga retry: %v", err)
	}

	bodies := hook.bodies()
	if len(bodies) != 3 {
		t.Fatalf("delivery attempts after retry = %d, want 3: %v", len(bodies), bodies)
	}
	if !contains(bodies[1], "https://mangadex.org/chapter/chapter-1") {
		t.Errorf("failed chapter was not retried: %s", bodies[1])
	}
	if !contains(bodies[2], "https://mangadex.org/chapter/chapter-2") {
		t.Errorf("later chapter was not delivered: %s", bodies[2])
	}
	if recovered == nil || *recovered != t2.UnixMilli() {
		t.Fatalf("watermark after retry = %v, want %d", recovered, t2.UnixMilli())
	}

	// poll() persists the returned watermark before the next run reloads it.
	subscription.Watermark = recovered

	again, err := manager.pollManga(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollManga after catch-up: %v", err)
	}
	if again != nil {
		t.Errorf("watermark after catch-up = %d, want it untouched", *again)
	}
	if got := len(hook.bodies()); got != 3 {
		t.Fatalf("delivered %d duplicates on a settled feed", got-3)
	}
}

func TestPollFeedStopsAtFirstFailureAndRetries(t *testing.T) {
	hook := newRecordingWebhook(t)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>
			<item><title>Old</title><link>https://example.com/old</link>
			<description>old</description><pubDate>Mon, 02 Jan 2023 15:04:05 GMT</pubDate></item>
			<item><title>New</title><link>https://example.com/new</link>
			<description>new</description><pubDate>Tue, 03 Jan 2023 15:04:05 GMT</pubDate></item>
			</channel></rss>`)
	}))
	t.Cleanup(feedServer.Close)

	manager := newTestManager(t, hook.server.URL, "")

	oldItem := time.Date(2023, 1, 2, 15, 4, 5, 0, time.UTC)
	newItem := time.Date(2023, 1, 3, 15, 4, 5, 0, time.UTC)

	watermark := oldItem.Add(-time.Hour).UnixMilli()
	subscription := &store.Subscription{
		ID:              "sub-1",
		Kind:            store.KindRSS,
		Label:           "Feed",
		Source:          feedServer.URL,
		Webhook:         "main",
		IntervalSeconds: 60,
		Enabled:         true,
		Watermark:       &watermark,
	}

	hook.respond(http.StatusInternalServerError)

	failed, err := manager.pollFeed(context.Background(), subscription, discardLogger())
	if err == nil || !contains(err.Error(), `deliver feed item "Old"`) {
		t.Fatalf("pollFeed error = %v, want it to name Old", err)
	}
	if failed == nil || *failed != watermark {
		t.Fatalf("watermark after failure = %v, want it unchanged at %d", failed, watermark)
	}
	if got := len(hook.bodies()); got != 1 {
		t.Fatalf("delivery attempts after failure = %d, want 1", got)
	}

	hook.respond(http.StatusNoContent)

	recovered, err := manager.pollFeed(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollFeed retry: %v", err)
	}

	bodies := hook.bodies()
	if len(bodies) != 3 {
		t.Fatalf("delivery attempts after retry = %d, want 3: %v", len(bodies), bodies)
	}
	if !contains(bodies[1], "Old") {
		t.Errorf("failed item was not retried: %s", bodies[1])
	}
	if !contains(bodies[2], "New") {
		t.Errorf("later item was not delivered: %s", bodies[2])
	}
	if recovered == nil || *recovered != newItem.UnixMilli() {
		t.Fatalf("watermark after retry = %v, want %d", recovered, newItem.UnixMilli())
	}

	// poll() persists the returned watermark before the next run reloads it.
	subscription.Watermark = recovered

	again, err := manager.pollFeed(context.Background(), subscription, discardLogger())
	if err != nil {
		t.Fatalf("pollFeed after catch-up: %v", err)
	}
	if again != nil {
		t.Errorf("watermark after catch-up = %d, want it untouched", *again)
	}
	if got := len(hook.bodies()); got != 3 {
		t.Fatalf("delivered %d duplicates on a settled feed", got-3)
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
