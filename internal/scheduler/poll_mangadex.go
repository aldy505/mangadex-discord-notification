package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/notify"
	"mangadex-discord-notification/internal/store"
)

// pollManga delivers every chapter newer than the subscription watermark.
func (m *Manager) pollManga(ctx context.Context, subscription *store.Subscription, logger *slog.Logger) (*int64, error) {
	if subscription.Watermark == nil {
		now := store.Now()
		logger.InfoContext(ctx, "initialised manga watermark, existing chapters will not be announced")

		return &now, nil
	}

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

	watermark := time.UnixMilli(*subscription.Watermark)
	chapters, err := m.mangadex.RecentChapters(ctx, subscription.Source, languages, &watermark, maxPagesPerPoll)
	if err != nil {
		return nil, fmt.Errorf("fetch mangadex feed: %w", err)
	}
	if len(chapters) == 0 {
		return nil, nil
	}

	var (
		newest   = *subscription.Watermark
		failures []string
	)

	for _, chapter := range chapters {
		if created := chapter.CreatedAt.UnixMilli(); created > newest {
			newest = created
		}

		title := subscription.Label
		if chapter.Title != "" {
			title += " - " + chapter.Title
		}

		number := chapter.Chapter
		if number == "" {
			number = "?"
		}

		update := notify.MangaUpdate{
			Title:   title,
			Chapter: number,
			URL:     "https://mangadex.org/chapter/" + chapter.ID,
		}

		logger.InfoContext(ctx, "delivering chapter",
			slog.String("chapter_id", chapter.ID),
			slog.String("chapter", number),
		)

		if err := notify.DeliverMangaUpdate(ctx, m.client, webhookURL, update, defaultAvatarURL); err != nil {
			logger.ErrorContext(ctx, "failed to deliver chapter", slog.String("error", err.Error()))
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return &newest, fmt.Errorf("deliver %d of %d chapters: %s", len(failures), len(chapters), strings.Join(failures, "; "))
	}

	return &newest, nil
}
