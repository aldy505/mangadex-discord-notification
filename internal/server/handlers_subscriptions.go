package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/feed"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/store"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type webhookResponse struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	MaskedURL string `json:"masked_url"`
	Default   bool   `json:"default"`
}

type subscriptionResponse struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Label           string   `json:"label"`
	Source          string   `json:"source"`
	Webhook         string   `json:"webhook"`
	Languages       []string `json:"languages"`
	IntervalSeconds int64    `json:"interval_seconds"`
	Enabled         bool     `json:"enabled"`
	LastCheckedAt   *int64   `json:"last_checked_at"`
	LastError       *string  `json:"last_error"`
	CreatedAt       int64    `json:"created_at"`
}

func toSubscriptionResponse(subscription store.Subscription) subscriptionResponse {
	response := subscriptionResponse{
		ID:              subscription.ID,
		Kind:            subscription.Kind,
		Label:           subscription.Label,
		Source:          subscription.Source,
		Webhook:         subscription.Webhook,
		Languages:       subscription.LanguageList(),
		IntervalSeconds: subscription.IntervalSeconds,
		Enabled:         subscription.Enabled,
		LastCheckedAt:   subscription.LastCheckedAt,
		CreatedAt:       subscription.CreatedAt,
	}
	if subscription.LastError != "" {
		lastError := subscription.LastError
		response.LastError = &lastError
	}

	return response
}

func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	defaultName := s.cfg.DefaultWebhook()

	webhooks := make([]webhookResponse, 0, len(s.cfg.Webhooks))
	for _, webhook := range s.cfg.Webhooks {
		webhooks = append(webhooks, webhookResponse{
			Name:      webhook.Name,
			Host:      webhook.Host(),
			MaskedURL: webhook.MaskedURL(),
			Default:   webhook.Name == defaultName,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"webhooks": webhooks})
}

func (s *Server) handleSubscriptionsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subscriptions, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list subscriptions", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions")

		return
	}

	response := make([]subscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		response = append(response, toSubscriptionResponse(subscription))
	}

	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": response})
}

func (s *Server) handleSubscriptionCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind            string   `json:"kind"`
		Webhook         string   `json:"webhook"`
		IntervalSeconds int64    `json:"interval_seconds"`
		Languages       []string `json:"languages"`
		MangaID         string   `json:"manga_id"`
		URL             string   `json:"url"`
		Label           string   `json:"label"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	webhookName := strings.TrimSpace(body.Webhook)
	if webhookName == "" {
		webhookName = s.cfg.DefaultWebhook()
	}
	if _, ok := s.cfg.WebhookByName(webhookName); !ok {
		writeFieldError(w, http.StatusBadRequest, "unknown webhook", map[string]string{
			"webhook": "choose one of the configured webhooks",
		})

		return
	}

	interval, err := s.normalizeInterval(body.IntervalSeconds)
	if err != nil {
		writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
			"interval_seconds": err.Error(),
		})

		return
	}

	ctx := r.Context()
	state := stateFrom(r)

	subscription := store.Subscription{
		ID:              uuid.NewString(),
		Webhook:         webhookName,
		IntervalSeconds: interval,
		Enabled:         true,
		CreatedBy:       state.Admin.ID,
		CreatedAt:       store.Now(),
	}

	switch body.Kind {
	case store.KindMangadex:
		mangaID, err := parseMangaID(body.MangaID)
		if err != nil {
			writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
				"manga_id": err.Error(),
			})

			return
		}

		languages, err := mangadex.NormalizeLanguages(body.Languages)
		if err != nil {
			writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
				"languages": err.Error(),
			})

			return
		}

		mangaCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		manga, err := s.mangadex.GetManga(mangaCtx, mangaID)
		cancel()
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to look up manga: "+err.Error())

			return
		}

		subscription.Kind = store.KindMangadex
		subscription.Source = mangaID
		subscription.Label = manga.DisplayTitle()

		codes := make([]string, 0, len(languages))
		for _, language := range languages {
			codes = append(codes, string(language))
		}
		subscription.SetLanguages(codes)

	case store.KindRSS:
		feedURL := strings.TrimSpace(body.URL)
		if err := validateFeedURL(feedURL); err != nil {
			writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
				"url": err.Error(),
			})

			return
		}

		feedCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		parsed, err := feed.Fetch(feedCtx, nil, feedURL)
		cancel()
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())

			return
		}

		label := strings.TrimSpace(body.Label)
		if label == "" {
			label = strings.TrimSpace(parsed.Title)
		}
		if label == "" {
			label = feedURL
		}

		subscription.Kind = store.KindRSS
		subscription.Source = feedURL
		subscription.Label = truncateString(label, 200)
		subscription.Languages = ""

	default:
		writeFieldError(w, http.StatusBadRequest, "unknown subscription kind", map[string]string{
			"kind": "must be mangadex or rss",
		})

		return
	}

	if err := s.store.CreateSubscription(ctx, &subscription); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "this subscription already exists for that webhook")

			return
		}
		s.logger.ErrorContext(ctx, "failed to create subscription", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to create subscription")

		return
	}

	s.auditState(r, store.ActionSubscriptionAdded, subscription.ID, map[string]any{
		"kind":    subscription.Kind,
		"label":   subscription.Label,
		"source":  subscription.Source,
		"webhook": subscription.Webhook,
	})

	if err := s.sched.Sync(ctx); err != nil {
		s.logger.ErrorContext(ctx, "failed to sync scheduler", slogError(err))
	}

	writeJSON(w, http.StatusCreated, toSubscriptionResponse(subscription))
}

func (s *Server) handleSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	subscription, err := s.store.GetSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "subscription not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to load subscription", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove subscription")

		return
	}

	if err := s.store.DeleteSubscription(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "subscription not found")

			return
		}
		s.logger.ErrorContext(ctx, "failed to delete subscription", slogError(err))
		writeError(w, http.StatusInternalServerError, "failed to remove subscription")

		return
	}

	s.auditState(r, store.ActionSubscriptionRemoved, id, map[string]any{
		"kind":   subscription.Kind,
		"label":  subscription.Label,
		"source": subscription.Source,
	})

	if err := s.sched.Sync(ctx); err != nil {
		s.logger.ErrorContext(ctx, "failed to sync scheduler", slogError(err))
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMangaPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	mangaID, err := parseMangaID(body.Input)
	if err != nil {
		writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
			"input": err.Error(),
		})

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	manga, err := s.mangadex.GetManga(ctx, mangaID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to look up manga: "+err.Error())

		return
	}

	languages := make([]string, 0, len(manga.AvailableLanguages))
	for _, language := range manga.AvailableLanguages {
		languages = append(languages, string(language))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"manga_id":            manga.ID,
		"title":               manga.DisplayTitle(),
		"cover_url":           manga.CoverURL(),
		"available_languages": languages,
		"last_chapter":        manga.LastChapter,
		"status":              manga.Status,
	})
}

func (s *Server) handleFeedPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	feedURL := strings.TrimSpace(body.URL)
	if err := validateFeedURL(feedURL); err != nil {
		writeFieldError(w, http.StatusBadRequest, err.Error(), map[string]string{
			"url": err.Error(),
		})

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	parsed, err := feed.Fetch(ctx, nil, feedURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	var latest any
	if published := feed.LatestItemTime(parsed); published != nil {
		latest = published.UnixMilli()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"title":          parsed.Title,
		"link":           parsed.Link,
		"description":    parsed.Description,
		"item_count":     len(parsed.Items),
		"latest_item_at": latest,
	})
}

// normalizeInterval clamps a requested poll interval to the allowed range.
func (s *Server) normalizeInterval(seconds int64) (int64, error) {
	if seconds == 0 {
		return int64(s.cfg.Poll.Seconds()), nil
	}

	minSeconds := int64(config.MinSubscriptionInterval.Seconds())
	maxSeconds := int64(config.MaxSubscriptionInterval.Seconds())

	if seconds < minSeconds {
		return 0, fmt.Errorf("interval must be at least %d seconds", minSeconds)
	}
	if seconds > maxSeconds {
		return 0, fmt.Errorf("interval must be at most %d seconds", maxSeconds)
	}

	return seconds, nil
}

// parseMangaID accepts a raw MangaDex UUID or a mangadex.org manga/title URL.
func parseMangaID(input string) (string, error) {
	candidate := strings.TrimSpace(input)
	if candidate == "" {
		return "", errors.New("enter a MangaDex URL or manga ID")
	}

	if parsed, err := uuid.Parse(candidate); err == nil {
		return parsed.String(), nil
	}

	parsedURL, err := url.Parse(candidate)
	if err != nil || parsedURL.Host == "" {
		return "", errors.New("enter a MangaDex URL or manga ID")
	}

	segments := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	for index, segment := range segments {
		if segment != "title" && segment != "manga" {
			continue
		}
		if index+1 >= len(segments) {
			continue
		}
		if parsed, err := uuid.Parse(segments[index+1]); err == nil {
			return parsed.String(), nil
		}
	}

	for index := len(segments) - 1; index >= 0; index-- {
		if parsed, err := uuid.Parse(segments[index]); err == nil {
			return parsed.String(), nil
		}
	}

	return "", errors.New("no manga ID found in that URL")
}

func validateFeedURL(raw string) error {
	if raw == "" {
		return errors.New("enter a feed URL")
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return errors.New("enter a valid feed URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("feed URL must use http or https")
	}

	return nil
}
