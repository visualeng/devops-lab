// Логи сервиса: JSON по одной строке на событие. Ровно в таком виде их
// потом ест Loki — без парсинга и без正则 на стороне агента.
package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func newLogger(out io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: logLevel()})
	// Версия и коммит в каждой строке: в логах видно, какой код жаловался
	return slog.New(handler).With(
		"service", serviceName,
		"version", version,
		"commit", commit,
	)
}

func logLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
