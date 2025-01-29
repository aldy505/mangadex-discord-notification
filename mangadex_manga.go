package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type MangadexMangaResponse struct {
	Result   string `json:"result"`
	Response string `json:"response"`
	Data     struct {
		Id         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Title                          map[LanguageKey]string `json:"title"`
			Description                    map[LanguageKey]string `json:"description"`
			IsLocked                       bool                   `json:"isLocked"`
			OriginalLanguage               string                 `json:"originalLanguage"`
			LastVolume                     string                 `json:"lastVolume"`
			LastChapter                    string                 `json:"lastChapter"`
			PublicationDemographic         interface{}            `json:"publicationDemographic"`
			Status                         string                 `json:"status"`
			Year                           int                    `json:"year"`
			ContentRating                  string                 `json:"contentRating"`
			State                          string                 `json:"state"`
			ChapterNumbersResetOnNewVolume bool                   `json:"chapterNumbersResetOnNewVolume"`
			CreatedAt                      time.Time              `json:"createdAt"`
			UpdatedAt                      time.Time              `json:"updatedAt"`
			Version                        int                    `json:"version"`
			AvailableTranslatedLanguages   []LanguageKey          `json:"availableTranslatedLanguages"`
			LatestUploadedChapter          string                 `json:"latestUploadedChapter"`
		} `json:"attributes"`
		Relationships []struct {
			Id   string `json:"id"`
			Type string `json:"type"`
		} `json:"relationships"`
	} `json:"data"`
}

func (m *MangadexClient) GetManga(ctx context.Context, mangaId string) (MangadexMangaResponse, error) {
	requestUrl := m.baseURL.JoinPath(fmt.Sprintf("/manga/%s", mangaId))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl.String(), nil)
	if err != nil {
		return MangadexMangaResponse{}, fmt.Errorf("failed to create mangadex manga request: %w", err)
	}

	response, err := m.httpClient.Do(request)
	if err != nil {
		return MangadexMangaResponse{}, fmt.Errorf("failed to send mangadex manga request: %w", err)
	}
	defer func() {
		if response.Body != nil {
			_ = response.Body.Close()
		}
	}()

	if response.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(response.Body)

		return MangadexMangaResponse{}, fmt.Errorf("mangadex manga responded with %d (%s)", response.StatusCode, string(responseBody))
	}

	var mangaResponse MangadexMangaResponse

	err = json.NewDecoder(response.Body).Decode(&mangaResponse)
	if err != nil {
		return MangadexMangaResponse{}, fmt.Errorf("failed to decode mangadex manga response: %w", err)
	}

	return mangaResponse, nil
}
