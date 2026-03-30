package logger

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, parseLevel(tt.input))
		})
	}
}

func TestInit_DoesNotPanic(t *testing.T) {
	// Ensure Init does not panic regardless of LOG_LEVEL
	t.Setenv("LOG_LEVEL", "debug")
	Init()
	t.Setenv("LOG_LEVEL", "")
	Init()
}
