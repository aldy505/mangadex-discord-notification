package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/mmcdole/gofeed"

	"mangadex-discord-notification/internal/feed"
	"mangadex-discord-notification/internal/notify"
	"mangadex-discord-notification/internal/store"
)

// feedCandidate is one feed item with the timestamp it is ordered by.
type feedCandidate struct {
	published time.Time
	item      *gofeed.Item
}

// pollFeed delivers every RSS/Atom item newer than the subscription watermark.
// The first poll of a new subscription announces only the newest item and seeds
// the watermark at that item's timestamp.
func (m *Manager) pollFeed(ctx context.Context, subscription *store.Subscription, logger *slog.Logger) (*int64, error) {
	webhookURL, err := m.webhookURL(subscription.Webhook)
	if err != nil {
		return nil, err
	}

	parsed, err := feed.Fetch(ctx, m.client, subscription.Source)
	if err != nil {
		return nil, err
	}

	candidates := make([]feedCandidate, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		published := item.PublishedParsed
		if published == nil {
			published = item.UpdatedParsed
		}
		if published == nil {
			continue
		}
		candidates = append(candidates, feedCandidate{published: *published, item: item})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].published.Before(candidates[j].published)
	})

	if subscription.Watermark == nil {
		return m.announceNewestItem(ctx, parsed, candidates, webhookURL, logger)
	}

	watermark := time.UnixMilli(*subscription.Watermark)

	pending := make([]feedCandidate, 0, len(candidates))
	for _, entry := range candidates {
		if entry.published.After(watermark) {
			pending = append(pending, entry)
		}
	}

	if len(pending) == 0 {
		return nil, nil
	}

	delivered := make([]int64, 0, len(pending))

	for _, entry := range pending {
		item := buildFeedItem(parsed, entry.published, entry.item)

		logger.InfoContext(ctx, "delivering feed item",
			slog.String("item_title", item.ItemTitle),
			slog.String("item_url", item.ItemURL),
		)

		if err := notify.DeliverFeedItem(ctx, m.client, webhookURL, item); err != nil {
			logger.ErrorContext(ctx, "failed to deliver feed item", slog.String("error", err.Error()))

			// Stop at the first failure: the later items are younger, so
			// leaving them for the next poll keeps the retry duplicate free.
			safe := advanceWatermark(*subscription.Watermark, delivered, &entry.published)

			return &safe, fmt.Errorf("deliver feed item %q: %w", item.ItemTitle, err)
		}

		delivered = append(delivered, entry.published.UnixMilli())
	}

	newest := advanceWatermark(*subscription.Watermark, delivered, nil)

	return &newest, nil
}

// announceNewestItem delivers the newest item of a new subscription so the
// operator sees the pipeline work end to end, then records the watermark at that
// item's timestamp. Older items stay unannounced.
func (m *Manager) announceNewestItem(
	ctx context.Context,
	parsed *gofeed.Feed,
	candidates []feedCandidate,
	webhookURL string,
	logger *slog.Logger,
) (*int64, error) {
	if len(candidates) == 0 {
		now := store.Now()
		logger.InfoContext(ctx, "first poll: no feed items yet, seeded watermark")

		return &now, nil
	}

	// The watermark comes from the feed clock, not from this host's clock.
	newestAt := candidates[len(candidates)-1].published

	for _, entry := range candidates {
		if !entry.published.Equal(newestAt) {
			continue
		}

		item := buildFeedItem(parsed, entry.published, entry.item)

		logger.InfoContext(ctx, "first poll: delivering newest feed item as baseline",
			slog.String("item_title", item.ItemTitle),
		)

		if err := notify.DeliverFeedItem(ctx, m.client, webhookURL, item); err != nil {
			logger.ErrorContext(ctx, "failed to deliver feed item", slog.String("error", err.Error()))

			return nil, fmt.Errorf("deliver feed item %q: %w", item.ItemTitle, err)
		}
	}

	watermark := newestAt.UnixMilli()

	return &watermark, nil
}

// buildFeedItem renders one feed announcement.
func buildFeedItem(parsed *gofeed.Feed, published time.Time, item *gofeed.Item) notify.FeedItem {
	feedItem := notify.FeedItem{
		ChannelTitle:       parsed.Title,
		ChannelDescription: parsed.Description,
		ChannelURL:         parsed.Link,
		ItemTitle:          item.Title,
		ItemDescription:    item.Description,
		ItemDate:           published.Format(time.RFC1123),
		ItemURL:            item.Link,
	}
	if feedItem.ItemTitle == "" {
		feedItem.ItemTitle = parsed.Title
	}
	if feedItem.ItemURL == "" {
		feedItem.ItemURL = parsed.Link
	}

	return feedItem
}
