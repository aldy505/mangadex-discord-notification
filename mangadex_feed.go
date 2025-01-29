package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/guregu/null/v5"
)

type MangadexFeedResponse struct {
	Result   string `json:"result"`
	Response string `json:"response"`
	Data     []struct {
		Id         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Volume             null.String `json:"volume"`
			Chapter            string      `json:"chapter"`
			Title              null.String `json:"title"`
			TranslatedLanguage LanguageKey `json:"translatedLanguage"`
			ExternalUrl        interface{} `json:"externalUrl"`
			PublishAt          time.Time   `json:"publishAt"`
			ReadableAt         time.Time   `json:"readableAt"`
			CreatedAt          time.Time   `json:"createdAt"`
			UpdatedAt          time.Time   `json:"updatedAt"`
			Pages              int         `json:"pages"`
			Version            int         `json:"version"`
		} `json:"attributes"`
		Relationships []struct {
			Id   string `json:"id"`
			Type string `json:"type"`
		} `json:"relationships"`
	} `json:"data"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func (m *MangadexClient) GetFeed(ctx context.Context, mangaId string, translatedLanguage []LanguageKey) (MangadexFeedResponse, error) {
	queryParams := url.Values{}
	for _, language := range translatedLanguage {
		queryParams.Add("translatedLanguage[]", string(language))
	}
	queryParams.Add("limit", "10")
	queryParams.Add("order[createdAt]", "desc")

	requestUrl := m.baseURL.JoinPath(fmt.Sprintf("/manga/%s/feed", mangaId))
	requestUrl.RawQuery = queryParams.Encode()
	slog.DebugContext(ctx, "Getting feed", slog.String("manga_id", mangaId), slog.Any("translated_language", translatedLanguage), slog.String("request_url", requestUrl.String()))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl.String(), nil)
	if err != nil {
		return MangadexFeedResponse{}, fmt.Errorf("failed to create mangadex feed request: %w", err)
	}

	response, err := m.httpClient.Do(request)
	if err != nil {
		return MangadexFeedResponse{}, fmt.Errorf("failed to send mangadex feed request: %w", err)
	}
	defer func() {
		if response.Body != nil {
			_ = response.Body.Close()
		}
	}()

	if response.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(response.Body)

		return MangadexFeedResponse{}, fmt.Errorf("mangadex feed responded with %d (%s)", response.StatusCode, string(responseBody))
	}

	var feedResponse MangadexFeedResponse

	err = json.NewDecoder(response.Body).Decode(&feedResponse)
	if err != nil {
		return MangadexFeedResponse{}, fmt.Errorf("failed to decode mangadex feed response: %w", err)
	}

	return feedResponse, nil
}
