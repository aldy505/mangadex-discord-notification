package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/notify"
	"mangadex-discord-notification/internal/store"
)

// pollManga delivers every chapter newer than the subscription watermark. The
// first poll of a new subscription announces only the newest chapter and seeds
// the watermark at that chapter's timestamp.
func (m *Manager) pollManga(ctx context.Context, subscription *store.Subscription, logger *slog.Logger) (*int64, error) {
	webhookURL, err := m.webhookURL(subscription.Webhook)
	if err != nil {
		return nil, err
	}

	languages := make([]mangadex.LanguageKey, 0, len(subscription.LanguageList()))
	for _, code := range subscription.LanguageList() {
		languages = append(languages, mangadex.LanguageKey(code))
	}
	if len(languages) == 0 {
		languages = []mangadex.LanguageKey{"en"}
	}

	if subscription.Watermark == nil {
		return m.announceNewestChapter(ctx, subscription, webhookURL, languages, logger)
	}

	watermark := time.UnixMilli(*subscription.Watermark)
	chapters, err := m.mangadex.RecentChapters(ctx, subscription.Source, languages, &watermark, maxPagesPerPoll)
	if err != nil {
		return nil, fmt.Errorf("fetch mangadex feed: %w", err)
	}
	if len(chapters) == 0 {
		return nil, nil
	}

	delivered := make([]int64, 0, len(chapters))

	for _, chapter := range chapters {
		number := chapter.Chapter
		if number == "" {
			number = "?"
		}

		logger.InfoContext(ctx, "delivering chapter",
			slog.String("chapter_id", chapter.ID),
			slog.String("chapter", number),
		)

		if err := notify.DeliverMangaUpdate(ctx, m.client, webhookURL, buildMangaUpdate(subscription, chapter), defaultAvatarURL); err != nil {
			logger.ErrorContext(ctx, "failed to deliver chapter", slog.String("error", err.Error()))

			// Stop at the first failure: the later chapters are younger, so
			// leaving them for the next poll keeps the retry duplicate free.
			safe := advanceWatermark(*subscription.Watermark, delivered, &chapter.CreatedAt)

			return &safe, fmt.Errorf("deliver chapter %s: %w", chapter.ID, err)
		}

		delivered = append(delivered, chapter.CreatedAt.UnixMilli())
	}

	newest := advanceWatermark(*subscription.Watermark, delivered, nil)

	return &newest, nil
}

// announceNewestChapter delivers the newest chapter of a new subscription so
// the operator sees the pipeline work end to end, then records the watermark at
// that chapter's timestamp. Older chapters stay unannounced.
func (m *Manager) announceNewestChapter(
	ctx context.Context,
	subscription *store.Subscription,
	webhookURL string,
	languages []mangadex.LanguageKey,
	logger *slog.Logger,
) (*int64, error) {
	// Without a watermark the client walks a single page, oldest first.
	chapters, err := m.mangadex.RecentChapters(ctx, subscription.Source, languages, nil, 1)
	if err != nil {
		return nil, fmt.Errorf("fetch mangadex feed: %w", err)
	}
	if len(chapters) == 0 {
		now := store.Now()
		logger.InfoContext(ctx, "first poll: no chapters yet, seeded watermark")

		return &now, nil
	}

	// The watermark comes from the feed clock, not from this host's clock.
	newestAt := chapters[len(chapters)-1].CreatedAt

	for _, chapter := range chapters {
		if !chapter.CreatedAt.Equal(newestAt) {
			continue
		}

		logger.InfoContext(ctx, "first poll: delivering newest chapter as baseline",
			slog.String("chapter_id", chapter.ID),
		)

		if err := notify.DeliverMangaUpdate(ctx, m.client, webhookURL, buildMangaUpdate(subscription, chapter), defaultAvatarURL); err != nil {
			logger.ErrorContext(ctx, "failed to deliver chapter", slog.String("error", err.Error()))

			return nil, fmt.Errorf("deliver chapter %s: %w", chapter.ID, err)
		}
	}

	watermark := newestAt.UnixMilli()

	return &watermark, nil
}

// buildMangaUpdate renders one chapter announcement.
func buildMangaUpdate(subscription *store.Subscription, chapter mangadex.FeedItem) notify.MangaUpdate {
	title := subscription.Label
	if chapter.Title != "" {
		title += " - " + chapter.Title
	}

	number := chapter.Chapter
	if number == "" {
		number = "?"
	}

	return notify.MangaUpdate{
		Title:   title,
		Chapter: number,
		URL:     "https://mangadex.org/chapter/" + chapter.ID,
	}
}
