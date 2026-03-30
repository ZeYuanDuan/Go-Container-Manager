package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Init sets up the global slog logger with JSON output and the level from LOG_LEVEL env var.
func Init() {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
