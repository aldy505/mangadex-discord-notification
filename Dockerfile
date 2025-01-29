FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY . .

RUN go build -o /app/mangadex-discord-notification -ldflags="-s -w" .

FROM alpine:latest AS runtime

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/mangadex-discord-notification /app/mangadex-discord-notification

USER nobody nobody

ENTRYPOINT ["/app/mangadex-discord-notification"]