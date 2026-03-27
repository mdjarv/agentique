package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

// LevelTrace is below Debug, for high-frequency messages like ws polling.
const LevelTrace = slog.Level(-8)

// Trace logs at trace level.
func Trace(msg string, args ...any) {
	slog.Log(context.Background(), LevelTrace, msg, args...)
}

// Init sets up charmbracelet/log as the default slog handler with colored output.
// Level is parsed from levelStr ("trace", "debug", "info", "warn", "error"); defaults to info.
func Init(levelStr string) {
	level := parseLevel(levelStr)

	handler := log.NewWithOptions(os.Stderr, log.Options{
		Level:           log.Level(level),
		ReportTimestamp: true,
	})

	styles := log.DefaultStyles()
	styles.Levels[log.Level(LevelTrace)] = lipgloss.NewStyle().
		SetString("TRAC").
		Bold(true).
		MaxWidth(4).
		Foreground(lipgloss.Color("241"))
	handler.SetStyles(styles)

	slog.SetDefault(slog.New(handler))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
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
