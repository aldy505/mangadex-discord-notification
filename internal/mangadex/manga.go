package mangadex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"
)

// Manga is the subset of a MangaDex manga entry this service uses.
type Manga struct {
	ID                     string
	Titles                 map[LanguageKey]string
	Description            map[LanguageKey]string
	Status                 string
	Year                   int
	ContentRating          string
	AvailableLanguages     []LanguageKey
	LatestUploadedChapter  string
	LastChapter            string
	CoverFileName          string
	OriginalLanguage       string
	PublicationDemographic string
}

// DisplayTitle prefers the English title, then the original one, then any
// title present.
func (m Manga) DisplayTitle() string {
	if title := m.Titles["en"]; title != "" {
		return title
	}

	for _, title := range m.Titles {
		if title != "" {
			return title
		}
	}

	return m.ID
}

// CoverURL returns the best available cover image URL, or an empty string.
func (m Manga) CoverURL() string {
	if m.CoverFileName == "" {
		return ""
	}

	return "https://uploads.mangadex.org/covers/" + url.PathEscape(m.ID) + "/" + url.PathEscape(m.CoverFileName) + ".512.jpg"
}

type mangaResponse struct {
	Result   string `json:"result"`
	Response string `json:"response"`
	Data     struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Title                          map[LanguageKey]string `json:"title"`
			Description                    map[LanguageKey]string `json:"description"`
			IsLocked                       bool                   `json:"isLocked"`
			OriginalLanguage               string                 `json:"originalLanguage"`
			LastVolume                     string                 `json:"lastVolume"`
			LastChapter                    string                 `json:"lastChapter"`
			PublicationDemographic         *string                `json:"publicationDemographic"`
			Status                         string                 `json:"status"`
			Year                           *int                   `json:"year"`
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
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				FileName string `json:"fileName"`
			} `json:"attributes"`
		} `json:"relationships"`
	} `json:"data"`
}

// GetManga fetches a manga together with its cover art relationship.
func (c *Client) GetManga(ctx context.Context, mangaID string) (Manga, error) {
	query := url.Values{}
	query.Add("includes[]", "cover_art")

	request, err := c.newRequest(ctx, "/manga/"+url.PathEscape(mangaID), query)
	if err != nil {
		return Manga{}, err
	}

	if err := c.wait(ctx); err != nil {
		return Manga{}, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Manga{}, fmt.Errorf("send mangadex manga request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))

		return Manga{}, fmt.Errorf("mangadex manga responded with %d (%s)", response.StatusCode, string(body))
	}

	var payload mangaResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Manga{}, fmt.Errorf("decode mangadex manga response: %w", err)
	}

	manga := Manga{
		ID:                    payload.Data.ID,
		Titles:                payload.Data.Attributes.Title,
		Description:           payload.Data.Attributes.Description,
		Status:                payload.Data.Attributes.Status,
		ContentRating:         payload.Data.Attributes.ContentRating,
		AvailableLanguages:    payload.Data.Attributes.AvailableTranslatedLanguages,
		LatestUploadedChapter: payload.Data.Attributes.LatestUploadedChapter,
		LastChapter:           payload.Data.Attributes.LastChapter,
		OriginalLanguage:      payload.Data.Attributes.OriginalLanguage,
	}
	if payload.Data.Attributes.Year != nil {
		manga.Year = *payload.Data.Attributes.Year
	}
	if payload.Data.Attributes.PublicationDemographic != nil {
		manga.PublicationDemographic = *payload.Data.Attributes.PublicationDemographic
	}

	for _, relationship := range payload.Data.Relationships {
		if relationship.Type == "cover_art" {
			manga.CoverFileName = relationship.Attributes.FileName
			break
		}
	}

	return manga, nil
}
