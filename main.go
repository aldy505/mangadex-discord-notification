package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"
)

type MangaUpdate struct {
	MangaId string
	Title   string
	Chapter string
	URL     string
}

func main() {
	// Parse some config from the environment
	scheduleRunInterval := time.Hour
	scheduleRunIntervalString, ok := os.LookupEnv("SCHEDULE_RUN_INTERVAL")
	if ok && scheduleRunIntervalString != "" {
		var err error
		scheduleRunInterval, err = time.ParseDuration(scheduleRunIntervalString)
		if err != nil {
			panic(err)
		}
	}

	var configs []Config
	configFilePath, ok := os.LookupEnv("CONFIG_FILE_PATH")
	if !ok || configFilePath == "" {
		// See if the list of manga IDs is in the environment
		mangaIdsString, ok := os.LookupEnv("MANGA_IDS")
		if !ok || mangaIdsString == "" {
			panic("no config file path or manga IDs found in environment")
		}

		// Split by comma
		mangaIds := strings.Split(mangaIdsString, ",")
		var err error
		configs, err = NewFromMangaIds(mangaIds)
		if err != nil {
			panic(err)
		}
	} else {
		var err error
		configs, err = ParseConfigFromFile(configFilePath)
		if err != nil {
			panic(err)
		}
	}

	webhookUrl, ok := os.LookupEnv("WEBHOOK_URL")
	if !ok || webhookUrl == "" {
		panic("WEBHOOK_URL environment variable not set")
	}

	logLevelString, ok := os.LookupEnv("LOG_LEVEL")
	if ok && logLevelString != "" {
		switch logLevelString {
		case "debug":
			slog.SetLogLoggerLevel(slog.LevelDebug)
			break
		case "info":
			slog.SetLogLoggerLevel(slog.LevelInfo)
			break
		case "warn":
			slog.SetLogLoggerLevel(slog.LevelWarn)
			break
		case "error":
			slog.SetLogLoggerLevel(slog.LevelError)
			break
		}
	}

	mangadexClient, err := NewMangadexClient("https://api.mangadex.org/", nil)
	if err != nil {
		panic(err)
	}
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt)

	go func(scheduleRunInterval time.Duration, configs []Config) {
		previousScheduleRun := time.Now().Add(-1 * scheduleRunInterval)
		for {
			nextScheduleRun := time.Now().Add(scheduleRunInterval)
			ctx, cancel := context.WithDeadline(context.Background(), nextScheduleRun)
			slog.DebugContext(ctx, "Running scheduled run", slog.Time("next_schedule_run", nextScheduleRun))

			for _, config := range configs {
				childCtx, childCancel := context.WithTimeout(ctx, time.Minute)
				feedResponse, err := mangadexClient.GetFeed(childCtx, config.MangaId, config.TranslatedLanguages)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to get feed", slog.String("manga_id", config.MangaId), slog.String("error", err.Error()))
					childCancel()
					continue
				}

				var foundOne bool
				var update MangaUpdate
				for _, manga := range feedResponse.Data {
					if !foundOne && manga.Attributes.CreatedAt.After(previousScheduleRun) {
						update = MangaUpdate{
							MangaId: manga.Id,
							Title:   manga.Attributes.Title.ValueOrZero(),
							Chapter: manga.Attributes.Chapter,
							URL:     "https://mangadex.org/chapter/" + manga.Id,
						}
						foundOne = true
					}
				}

				if foundOne {
					mangaInfo, err := mangadexClient.GetManga(ctx, config.MangaId)
					if err != nil {
						slog.ErrorContext(ctx, "Failed to get manga info", slog.String("error", err.Error()))
						childCancel()
						continue
					}

					if update.Title == "" {
						update.Title = mangaInfo.Data.Attributes.Title["en"]
					} else {
						update.Title = mangaInfo.Data.Attributes.Title["en"] + " - " + update.Title
					}

					slog.InfoContext(ctx, "Found update", slog.Any("update", update))
					err = DeliverToDiscord(ctx, webhookUrl, update, "https://image.spreadshirtmedia.net/image-server/v1/compositions/T1599A1PA5076PT10X11Y7D318959464W4338H5200/views/1,width=550,height=550,appearanceId=1,backgroundColor=FFFFFF,noPt=true/laughing-cat-in-manga-kawaii-style-square-fridge-magnet.jpg")
					if err != nil {
						slog.ErrorContext(ctx, "Failed to deliver to discord", slog.String("error", err.Error()))
						childCancel()
						continue
					}
				}
			}

			cancel()
			slog.DebugContext(ctx, "Sleeping", slog.Duration("duration", nextScheduleRun.Sub(time.Now())))
			time.Sleep(nextScheduleRun.Sub(time.Now()))
		}
	}(scheduleRunInterval, configs)

	<-exitSignal
}
