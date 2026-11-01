package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

const (
	userAgent = "Mangadex-Discord-Notification/2.0"

	// maxContentLength is Discord's hard limit for message content.
	maxContentLength = 2000
	// maxUsernameLength is Discord's limit for a webhook message username.
	maxUsernameLength = 80
)

type discordWebhookObject struct {
	Username  string `json:"username,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Content   string `json:"content"`
}

// MangaUpdate is one chapter announcement.
type MangaUpdate struct {
	Title   string
	Chapter string
	URL     string
}

// FeedItem is one RSS/Atom item announcement.
type FeedItem struct {
	ChannelTitle       string
	ChannelDescription string
	ChannelURL         string
	ItemTitle          string
	ItemDescription    string
	ItemDate           string
	ItemURL            string
}

// DeliverMangaUpdate posts a chapter announcement to a Discord webhook.
func DeliverMangaUpdate(ctx context.Context, client *http.Client, webhookURL string, update MangaUpdate, avatarURL string) error {
	var builder strings.Builder
	builder.WriteString("**")
	builder.WriteString(update.Title)
	builder.WriteString(" | Ch. ")
	builder.WriteString(update.Chapter)
	builder.WriteString("**\n")
	builder.WriteString(update.URL)

	return deliver(ctx, client, webhookURL, discordWebhookObject{
		Username:  "Mangadex",
		AvatarURL: avatarURL,
		Content:   builder.String(),
	})
}

// DeliverFeedItem posts an RSS item to a Discord webhook. HTML in the item
// description is converted to Markdown first.
func DeliverFeedItem(ctx context.Context, client *http.Client, webhookURL string, item FeedItem) error {
	content, err := md.NewConverter("", true, nil).ConvertString(item.ItemDescription)
	if err != nil {
		return fmt.Errorf("convert feed item HTML to markdown: %w", err)
	}
	content = strings.TrimSpace(content)
	if content != "" {
		content += "\n\n"
	}

	var builder strings.Builder
	builder.WriteString("📰 **")
	builder.WriteString(item.ItemTitle)
	builder.WriteString("**\n\n")
	builder.WriteString(content)
	builder.WriteString("Read more: ")
	builder.WriteString(item.ItemURL)

	username := item.ChannelTitle
	if username == "" {
		username = "RSS"
	}

	return deliver(ctx, client, webhookURL, discordWebhookObject{
		Username: username,
		Content:  builder.String(),
	})
}

func deliver(ctx context.Context, client *http.Client, webhookURL string, payload discordWebhookObject) error {
	payload.Content = truncate(payload.Content, maxContentLength)
	payload.Username = truncate(payload.Username, maxUsernameLength)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)

	if client == nil {
		client = http.DefaultClient
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send discord webhook: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 512))

		return fmt.Errorf("discord webhook responded with %d (%s)", response.StatusCode, string(responseBody))
	}

	return nil
}

// truncate shortens content to Discord's limit without splitting a rune.
func truncate(content string, limit int) string {
	if utf8.RuneCountInString(content) <= limit {
		return content
	}

	const ellipsis = "…"
	runes := []rune(content)

	return string(runes[:limit-len([]rune(ellipsis))]) + ellipsis
}
