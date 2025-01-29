# Mangadex Discord Notification

This is a small utility that will notify you when a new chapter is released on your favorite manga on Mangadex.

Best with Docker Compose:
```yaml
services:
  mangadex-discord-notification:
    image: ghcr.io/aldy505/mangadex-discord-notification:latest
    restart: unless-stopped
    environment:
      SCHEDULE_RUN_INTERVAL: 1h # Optional, default is 1h
      CONFIG_FILE_PATH: /app/config.json # Optional, if not set, will use `MANGA_IDS`
      MANGA_IDS: manga_id_1,manga_id_2 # Optional, will fail to start if both `CONFIG_FILE_PATH` and `MANGA_IDS` are not set
      WEBHOOK_URL: https://discord.com/api/webhooks/webhook_id/webhook_token # Required
      LOG_LEVEL: info # Optional, default is info
    volumes:
      - ./config.json:/app/config.json:ro # Optional, if not set, will use `MANGA_IDS`
```

Copy the `config.example.json` to `config.json` and fill in the required fields.

## License

[MIT](./LICENSE)