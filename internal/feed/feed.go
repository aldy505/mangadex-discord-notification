// Package feed fetches and parses RSS, Atom, and JSON feeds.
package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

const userAgent = "Mangadex-Discord-Notification/2.0 (+feed)"

// Fetch downloads and parses a feed.
func Fetch(ctx context.Context, client *http.Client, url string) (*gofeed.Feed, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create feed request: %w", err)
	}
	request.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.8")
	request.Header.Set("User-Agent", userAgent)

	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))

		return nil, fmt.Errorf("feed responded with %d (%s)", response.StatusCode, string(body))
	}

	parsed, err := gofeed.NewParser().Parse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	return parsed, nil
}

// LatestItemTime returns the newest publication time in the feed, if any.
func LatestItemTime(parsed *gofeed.Feed) *time.Time {
	var latest *time.Time
	for _, item := range parsed.Items {
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		if published == nil {
			continue
		}
		if latest == nil || published.After(*latest) {
			value := *published
			latest = &value
		}
	}

	return latest
}
