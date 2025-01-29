package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
)

type discordWebhookObject struct {
	Username  string               `json:"username"`
	AvatarURL string               `json:"avatar_url"`
	Content   string               `json:"content"`
	Embeds    []discordEmbedObject `json:"embeds,omitempty"`
}

type discordEmbedObject struct {
	Author      discordAuthorObject  `json:"author"`
	Title       string               `json:"title"`
	Url         string               `json:"url"`
	Description string               `json:"description"`
	Color       int                  `json:"color"`
	Fields      []discordFieldObject `json:"fields"`
	Thumbnail   discordUrlObject     `json:"thumbnail"`
	Image       discordUrlObject     `json:"image"`
}

type discordAuthorObject struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	IconURL string `json:"icon_url"`
}

type discordFieldObject struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordUrlObject struct {
	Url string `json:"url"`
}

var discordTemplate = template.Must(template.New("discord").Parse("📰 **{{.Title}}**\n\n{{.Content}}Read more: {{.URL}}"))

type discordTemplateData struct {
	Title   string
	Content string
	URL     string
}

func DeliverToDiscord(ctx context.Context, webhookURL string, feedItem MangaUpdate, customLogo string) error {
	// Prepare the webhook object
	var sb strings.Builder
	sb.WriteString("**" + feedItem.Title + " | Ch. " + feedItem.Chapter + "**\n")
	sb.WriteString(feedItem.URL)

	webhookObject := discordWebhookObject{
		Username:  "Mangadex",
		AvatarURL: customLogo,
		Content:   sb.String(),
	}

	body, err := json.Marshal(webhookObject)
	if err != nil {
		return fmt.Errorf("failed to marshal discord webhook object: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create discord webhook request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Mangadex-Discord-Notification/1.0")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to send discord webhook: %w", err)
	}
	defer func() {
		if response.Body != nil {
			_ = response.Body.Close()
		}
	}()

	if response.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(response.Body)

		return fmt.Errorf("discord webhook responded with %d (%s)", response.StatusCode, string(responseBody))
	}

	return nil
}
