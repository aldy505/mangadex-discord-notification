package mangadex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/guregu/null/v6"
)

// FeedItem is one chapter from a manga feed.
type FeedItem struct {
	ID          string
	Chapter     string
	Title       string
	Language    LanguageKey
	ExternalURL string
	CreatedAt   time.Time
}

// FeedPage is a single page of a manga feed.
type FeedPage struct {
	Items  []FeedItem
	Limit  int
	Offset int
	Total  int
}

type feedResponse struct {
	Result   string `json:"result"`
	Response string `json:"response"`
	Data     []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Volume             null.String `json:"volume"`
			Chapter            string      `json:"chapter"`
			Title              null.String `json:"title"`
			TranslatedLanguage LanguageKey `json:"translatedLanguage"`
			ExternalURL        null.String `json:"externalUrl"`
			PublishAt          time.Time   `json:"publishAt"`
			ReadableAt         time.Time   `json:"readableAt"`
			CreatedAt          time.Time   `json:"createdAt"`
			UpdatedAt          time.Time   `json:"updatedAt"`
			Pages              int         `json:"pages"`
			Version            int         `json:"version"`
		} `json:"attributes"`
		Relationships []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"relationships"`
	} `json:"data"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// GetFeed fetches one page of a manga's chapter feed, newest first.
func (c *Client) GetFeed(ctx context.Context, mangaID string, languages []LanguageKey, limit, offset int) (FeedPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := url.Values{}
	for _, language := range languages {
		query.Add("translatedLanguage[]", string(language))
	}
	query.Add("limit", strconv.Itoa(limit))
	query.Add("offset", strconv.Itoa(offset))
	query.Add("order[createdAt]", "desc")
	query.Add("includes[]", "scanlation_group")

	request, err := c.newRequest(ctx, "/manga/"+url.PathEscape(mangaID)+"/feed", query)
	if err != nil {
		return FeedPage{}, err
	}

	if err := c.wait(ctx); err != nil {
		return FeedPage{}, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return FeedPage{}, fmt.Errorf("send mangadex feed request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))

		return FeedPage{}, fmt.Errorf("mangadex feed responded with %d (%s)", response.StatusCode, string(body))
	}

	var payload feedResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return FeedPage{}, fmt.Errorf("decode mangadex feed response: %w", err)
	}

	page := FeedPage{Limit: payload.Limit, Offset: payload.Offset, Total: payload.Total}
	page.Items = make([]FeedItem, 0, len(payload.Data))
	for _, item := range payload.Data {
		page.Items = append(page.Items, FeedItem{
			ID:        item.ID,
			Chapter:   item.Attributes.Chapter,
			Title:     item.Attributes.Title.String,
			Language:  item.Attributes.TranslatedLanguage,
			CreatedAt: item.Attributes.CreatedAt,
		})
	}

	return page, nil
}

// RecentChapters walks the feed until it reaches chapters older than the
// watermark, returning them oldest first. A nil watermark returns at most one
// page so a new subscription does not backfill its whole history.
func (c *Client) RecentChapters(ctx context.Context, mangaID string, languages []LanguageKey, watermark *time.Time, maxPages int) ([]FeedItem, error) {
	const pageSize = 100

	var (
		offset int
		items  []FeedItem
	)

	for page := 0; page < maxPages; page++ {
		feedPage, err := c.GetFeed(ctx, mangaID, languages, pageSize, offset)
		if err != nil {
			return nil, err
		}

		oldest := time.Time{}
		for _, item := range feedPage.Items {
			if oldest.IsZero() || item.CreatedAt.Before(oldest) {
				oldest = item.CreatedAt
			}
			if watermark != nil && !item.CreatedAt.After(*watermark) {
				continue
			}
			items = append(items, item)
		}

		if watermark == nil {
			break
		}
		if len(feedPage.Items) == 0 || feedPage.Offset+len(feedPage.Items) >= feedPage.Total {
			break
		}
		if !oldest.IsZero() && !oldest.After(*watermark) {
			break
		}

		offset += len(feedPage.Items)
	}

	// The API returns newest first; delivery reads best oldest first.
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}

	return items, nil
}
