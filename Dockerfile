FROM node:24-alpine AS web

WORKDIR /web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web /web/dist ./web/dist

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mangadex-discord-notification .

FROM alpine:latest AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S otaku && adduser -S otaku -G otaku && \
    mkdir -p /app/data && chown -R otaku:otaku /app

WORKDIR /app

COPY --from=builder /out/mangadex-discord-notification /app/mangadex-discord-notification

USER otaku

EXPOSE 8080

VOLUME ["/app/data"]

ENTRYPOINT ["/app/mangadex-discord-notification"]
CMD ["serve"]
