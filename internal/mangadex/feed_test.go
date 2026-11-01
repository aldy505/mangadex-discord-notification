package mangadex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// newFeedServer serves a MangaDex style feed API over a fixed, newest first
// item list.
func newFeedServer(t *testing.T, items []time.Time) (*Client, func() int) {
	t.Helper()

	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if got := r.URL.Query().Get("translatedLanguage[]"); got != "en" {
			t.Errorf("language filter = %q", got)
		}

		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Errorf("parse offset: %v", err)

			return
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			t.Errorf("parse limit: %v", err)

			return
		}

		end := min(offset+limit, len(items))
		page := []time.Time{}
		if offset < len(items) {
			page = items[offset:end]
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":"ok","response":"collection","limit":%d,"offset":%d,"total":%d,"data":[`, limit, offset, len(items))
		for index, createdAt := range page {
			if index > 0 {
				fmt.Fprint(w, ",")
			}
			stamp := createdAt.Format(time.RFC3339Nano)
			fmt.Fprintf(w, `{"id":"chapter-%d","type":"chapter","attributes":{"chapter":"%d","title":"Ch%d",
				"translatedLanguage":"en","publishAt":%q,"readableAt":%q,"createdAt":%q,"updatedAt":%q,"pages":10,"version":1}}`,
				offset+index, offset+index, offset+index, stamp, stamp, stamp, stamp)
		}
		fmt.Fprint(w, `]}`)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return client, func() int { return requests }
}

// newestFirstItems returns `count` timestamps one minute apart, newest first.
func newestFirstItems(count int) []time.Time {
	base := time.Now().UTC().Truncate(time.Second)
	items := make([]time.Time, count)
	for index := range count {
		items[index] = base.Add(-time.Duration(index) * time.Minute)
	}

	return items
}

func TestRecentChaptersWithoutWatermarkReadsOnePage(t *testing.T) {
	items := newestFirstItems(150)
	client, requests := newFeedServer(t, items)

	chapters, err := client.RecentChapters(context.Background(), "manga-id", []LanguageKey{"en"}, nil, 5)
	if err != nil {
		t.Fatalf("RecentChapters: %v", err)
	}

	if len(chapters) != 100 {
		t.Fatalf("chapters = %d, want a single 100 item page", len(chapters))
	}
	if requests() != 1 {
		t.Fatalf("requests = %d, want 1", requests())
	}
}

func TestRecentChaptersWalksPagesPastWatermark(t *testing.T) {
	items := newestFirstItems(150)
	client, _ := newFeedServer(t, items)

	watermark := items[120]
	chapters, err := client.RecentChapters(context.Background(), "manga-id", []LanguageKey{"en"}, &watermark, 5)
	if err != nil {
		t.Fatalf("RecentChapters: %v", err)
	}

	// Items 0..119 are newer than the watermark; 120 is the first page-two
	// item older than it, which stops the walk.
	if len(chapters) != 120 {
		t.Fatalf("chapters = %d, want 120", len(chapters))
	}
	if !chapters[0].CreatedAt.Equal(items[119]) {
		t.Errorf("first chapter = %s, want the oldest matching item %s", chapters[0].CreatedAt, items[119])
	}
	if !chapters[len(chapters)-1].CreatedAt.Equal(items[0]) {
		t.Errorf("last chapter = %s, want the newest item %s", chapters[len(chapters)-1].CreatedAt, items[0])
	}
	for _, chapter := range chapters {
		if !chapter.CreatedAt.After(watermark) {
			t.Fatalf("returned a chapter at or before the watermark: %s", chapter.CreatedAt)
		}
	}
}

func TestRecentChaptersStopsAtPageBudget(t *testing.T) {
	items := newestFirstItems(250)
	client, requests := newFeedServer(t, items)

	watermark := items[249]
	chapters, err := client.RecentChapters(context.Background(), "manga-id", []LanguageKey{"en"}, &watermark, 2)
	if err != nil {
		t.Fatalf("RecentChapters: %v", err)
	}

	if len(chapters) != 200 {
		t.Fatalf("chapters = %d, want two pages (200)", len(chapters))
	}
	if requests() != 2 {
		t.Fatalf("requests = %d, want 2", requests())
	}
}

func TestNormalizeLanguages(t *testing.T) {
	languages, err := NormalizeLanguages([]string{"EN", "en", " fr "})
	if err != nil {
		t.Fatalf("NormalizeLanguages: %v", err)
	}
	if len(languages) != 2 || languages[0] != "en" || languages[1] != "fr" {
		t.Fatalf("languages = %v", languages)
	}

	if _, err := NormalizeLanguages([]string{"not-a-language"}); err == nil {
		t.Fatal("expected an invalid language to be rejected")
	}

	languages, err = NormalizeLanguages(nil)
	if err != nil || len(languages) != 1 || languages[0] != "en" {
		t.Fatalf("default languages = %v (%v)", languages, err)
	}
}
