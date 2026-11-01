package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"mangadex-discord-notification/internal/config"
	"mangadex-discord-notification/internal/mangadex"
	"mangadex-discord-notification/internal/store"
)

// defaultAvatarURL is the image every chapter notification uses.
const defaultAvatarURL = "https://image.spreadshirtmedia.net/image-server/v1/compositions/T1599A1PA5076PT10X11Y7D318959464W4338H5200/views/1,width=550,height=550,appearanceId=1,backgroundColor=FFFFFF,noPt=true/laughing-cat-in-manga-kawaii-style-square-fridge-magnet.jpg"

// maxPagesPerPoll bounds how far back a single MangaDex poll walks the feed.
const maxPagesPerPoll = 5

// Manager owns one polling goroutine per enabled subscription.
type Manager struct {
	cfg      *config.Config
	store    *store.Store
	mangadex *mangadex.Client
	client   *http.Client
	logger   *slog.Logger

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	workers map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func NewManager(cfg *config.Config, st *store.Store, mangadexClient *mangadex.Client, client *http.Client, logger *slog.Logger) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		cfg:      cfg,
		store:    st,
		mangadex: mangadexClient,
		client:   client,
		logger:   logger,
		workers:  make(map[string]context.CancelFunc),
	}
}

// Start launches a worker for every enabled subscription.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()

		return errors.New("scheduler already started")
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	runCtx := m.ctx
	m.mu.Unlock()

	subscriptions, err := m.store.ListEnabledSubscriptions(runCtx)
	if err != nil {
		return err
	}

	for _, subscription := range subscriptions {
		m.startWorker(subscription)
	}

	m.logger.InfoContext(runCtx, "scheduler started", slog.Int("subscriptions", len(subscriptions)))

	return nil
}

// Sync reconciles running workers with the enabled subscriptions in the
// database. Call it after any subscription is added or removed.
func (m *Manager) Sync(ctx context.Context) error {
	m.mu.Lock()
	runCtx := m.ctx
	m.mu.Unlock()

	if runCtx == nil {
		return errors.New("scheduler not started")
	}

	subscriptions, err := m.store.ListEnabledSubscriptions(ctx)
	if err != nil {
		return err
	}

	desired := make(map[string]store.Subscription, len(subscriptions))
	for _, subscription := range subscriptions {
		desired[subscription.ID] = subscription
	}

	m.mu.Lock()
	for id, cancel := range m.workers {
		if _, ok := desired[id]; !ok {
			cancel()
			delete(m.workers, id)
		}
	}
	m.mu.Unlock()

	for _, subscription := range subscriptions {
		m.mu.Lock()
		_, running := m.workers[subscription.ID]
		m.mu.Unlock()

		if !running {
			m.startWorker(subscription)
		}
	}

	return nil
}

func (m *Manager) startWorker(subscription store.Subscription) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx == nil {
		return
	}
	if _, running := m.workers[subscription.ID]; running {
		return
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.workers[subscription.ID] = cancel

	m.wg.Add(1)
	go m.run(ctx, subscription)
}

func (m *Manager) run(ctx context.Context, subscription store.Subscription) {
	defer m.wg.Done()

	interval := time.Duration(subscription.IntervalSeconds) * time.Second
	if interval < config.MinSubscriptionInterval {
		interval = config.MinSubscriptionInterval
	}

	logger := m.logger.With(
		slog.String("subscription_id", subscription.ID),
		slog.String("kind", subscription.Kind),
		slog.String("label", subscription.Label),
		slog.Duration("interval", interval),
	)
	logger.DebugContext(ctx, "subscription worker started")

	// Spread the initial polls so a restart does not hammer the APIs.
	initialDelay := time.Duration(rand.Int64N(int64(30 * time.Second)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(initialDelay):
	}

	m.poll(ctx, subscription.ID, logger)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.DebugContext(ctx, "subscription worker stopped")

			return
		case <-ticker.C:
			m.poll(ctx, subscription.ID, logger)
		}
	}
}

// Wait blocks until every worker has stopped. Call it after cancelling the
// manager context.
func (m *Manager) Wait() {
	m.wg.Wait()
}

// Shutdown cancels every worker.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	for id, workerCancel := range m.workers {
		workerCancel()
		delete(m.workers, id)
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (m *Manager) poll(ctx context.Context, subscriptionID string, logger *slog.Logger) {
	// Re-read the subscription so the watermark is always current.
	subscription, err := m.store.GetSubscription(ctx, subscriptionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		logger.ErrorContext(ctx, "failed to load subscription", slog.String("error", err.Error()))
		sentry.CaptureException(err)

		return
	}
	if !subscription.Enabled {
		return
	}

	pollCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	checkedAt := store.Now()

	var watermark *int64
	switch subscription.Kind {
	case store.KindMangadex:
		watermark, err = m.pollManga(pollCtx, subscription, logger)
	case store.KindRSS:
		watermark, err = m.pollFeed(pollCtx, subscription, logger)
	default:
		err = errors.New("unknown subscription kind " + subscription.Kind)
	}

	if err != nil {
		logger.ErrorContext(ctx, "poll failed", slog.String("error", err.Error()))
		sentry.CaptureException(err)

		if updateErr := m.store.UpdateSubscriptionPoll(ctx, subscription.ID, watermark, checkedAt, err.Error()); updateErr != nil {
			// The subscription may have been deleted while this poll was running.
			if !errors.Is(updateErr, store.ErrNotFound) {
				logger.ErrorContext(ctx, "failed to record poll failure", slog.String("error", updateErr.Error()))
			}
		}

		return
	}

	if updateErr := m.store.UpdateSubscriptionPoll(ctx, subscription.ID, watermark, checkedAt, ""); updateErr != nil {
		if !errors.Is(updateErr, store.ErrNotFound) {
			logger.ErrorContext(ctx, "failed to record poll result", slog.String("error", updateErr.Error()))
		}
	}
}

func (m *Manager) webhookURL(name string) (string, error) {
	webhook, ok := m.cfg.WebhookByName(name)
	if !ok {
		return "", errors.New("webhook " + name + " is not configured anymore")
	}

	return webhook.URL, nil
}
