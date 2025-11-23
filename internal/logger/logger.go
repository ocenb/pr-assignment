package logger

import (
	"log/slog"
	"os"

	"github.com/ocenb/pr-assignment/internal/config"
)

func New(cfg *config.Config) *slog.Logger {
	logLevel := slog.Level(cfg.Log.Level)

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: cfg.Environment == "local" || logLevel == slog.LevelDebug,
	}

	var handler slog.Handler

	if cfg.Log.Handler == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler).With(slog.String("env", cfg.Environment))
}
