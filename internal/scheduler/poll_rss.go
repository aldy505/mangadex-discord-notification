package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/mmcdole/gofeed"
	"strings"
	"time"

	"mangadex-discord-notification/internal/feed"
	"mangadex-discord-notification/internal/notify"
	"mangadex-discord-notification/internal/store"
)

// pollFeed delivers every RSS/Atom item newer than the subscription watermark.
func (m *Manager) pollFeed(ctx context.Context, subscription *store.Subscription, logger *slog.Logger) (*int64, error) {
	if subscription.Watermark == nil {
		now := store.Now()
		logger.InfoContext(ctx, "initialised feed watermark, existing items will not be announced")

		return &now, nil
	}

	webhookURL, err := m.webhookURL(subscription.Webhook)
	if err != nil {
		return nil, err
	}

	parsed, err := feed.Fetch(ctx, m.client, subscription.Source)
	if err != nil {
		return nil, err
	}

	watermark := time.UnixMilli(*subscription.Watermark)

	type candidate struct {
		published time.Time
		item      *gofeed.Item
	}

	candidates := make([]candidate, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		if published == nil {
			continue
		}
		if published.After(watermark) {
			candidates = append(candidates, candidate{published: *published, item: item})
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].published.Before(candidates[j].published)
	})

	var (
		newest   = *subscription.Watermark
		failures []string
	)

	for _, entry := range candidates {
		if millis := entry.published.UnixMilli(); millis > newest {
			newest = millis
		}

		item := notify.FeedItem{
			ChannelTitle:       parsed.Title,
			ChannelDescription: parsed.Description,
			ChannelURL:         parsed.Link,
			ItemTitle:          entry.item.Title,
			ItemDescription:    entry.item.Description,
			ItemDate:           entry.published.Format(time.RFC1123),
			ItemURL:            entry.item.Link,
		}
		if item.ItemTitle == "" {
			item.ItemTitle = parsed.Title
		}
		if item.ItemURL == "" {
			item.ItemURL = parsed.Link
		}

		logger.InfoContext(ctx, "delivering feed item",
			slog.String("item_title", item.ItemTitle),
			slog.String("item_url", item.ItemURL),
		)

		if err := notify.DeliverFeedItem(ctx, m.client, webhookURL, item); err != nil {
			logger.ErrorContext(ctx, "failed to deliver feed item", slog.String("error", err.Error()))
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return &newest, fmt.Errorf("deliver %d of %d items: %s", len(failures), len(candidates), strings.Join(failures, "; "))
	}

	return &newest, nil
}
