package mangadex

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	// APIBaseURL is the public MangaDex API endpoint.
	APIBaseURL = "https://api.mangadex.org/"

	userAgent = "Mangadex-Discord-Notification/2.0"

	// requestInterval keeps the client under MangaDex's 5 requests/second
	// budget even when many subscriptions poll at once.
	requestInterval = 250 * time.Millisecond
)

// Client is a rate limited MangaDex API client.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client

	mu       sync.Mutex
	lastCall time.Time
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse mangadex base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{baseURL: parsedURL, httpClient: httpClient}, nil
}

// wait blocks until the next request slot is available.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	next := c.lastCall.Add(requestInterval)
	// Reserve the slot we are about to wait for, so the next caller also waits
	// a full interval instead of firing alongside this one.
	if now.Before(next) {
		c.lastCall = next
	} else {
		c.lastCall = now
	}
	c.mu.Unlock()

	if now.Before(next) {
		timer := time.NewTimer(next.Sub(now))
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	return nil
}

func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
	requestURL := c.baseURL.JoinPath(path)
	if len(query) > 0 {
		requestURL.RawQuery = query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create mangadex request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")

	return request, nil
}
