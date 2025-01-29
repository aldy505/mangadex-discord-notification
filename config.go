package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	MangaId             string        `json:"manga_id"`
	TranslatedLanguages []LanguageKey `json:"translated_languages"`
}

func ParseConfigFromFile(path string) ([]Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("failed to close config file", slog.String("error", err.Error()))
		}
	}()

	var configs []Config
	err = json.NewDecoder(file).Decode(&configs)
	if err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	// Validate the config, make sure at least `manga_id` is present and not empty.
	for i := 0; i < len(configs); i++ {
		config := configs[i]
		if config.MangaId == "" {
			return nil, fmt.Errorf("config: manga ID is empty on index %d", i)
		}

		if len(config.TranslatedLanguages) == 0 {
			configs[i].TranslatedLanguages = []LanguageKey{"en"}
		} else {
			for _, language := range config.TranslatedLanguages {
				if !language.IsValid() {
					return nil, fmt.Errorf("config: invalid language on index %d: %s", i, language)
				}
			}
		}
	}

	return configs, nil
}

func NewFromMangaIds(mangaIds []string) ([]Config, error) {
	var configs = make([]Config, len(mangaIds))
	for i := 0; i < len(mangaIds); i++ {
		if mangaIds[i] == "" {
			continue
		}

		configs[i].MangaId = mangaIds[i]
		configs[i].TranslatedLanguages = []LanguageKey{"en"}
	}

	return configs, nil
}
