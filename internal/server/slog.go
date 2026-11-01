package server

import (
	"log/slog"
)

func slogError(err error) slog.Attr {
	return slog.String("error", err.Error())
}

func slogString(key, value string) slog.Attr {
	return slog.String(key, value)
}
