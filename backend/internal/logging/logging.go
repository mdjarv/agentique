package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

// LevelTrace is below Debug, for high-frequency messages like ws polling.
const LevelTrace = slog.Level(-8)

// OutputMode controls where log output is directed.
type OutputMode string

const (
	OutputAuto     OutputMode = "auto"     // detect journald, else file+stderr
	OutputJournald OutputMode = "journald" // plain text to stderr, no file
	OutputFile     OutputMode = "file"     // colored stderr + JSON file (original behavior)
	OutputStdout   OutputMode = "stdout"   // plain text to stdout only
)

// Trace logs at trace level.
func Trace(msg string, args ...any) {
	slog.Log(context.Background(), LevelTrace, msg, args...)
}

// Init sets up logging with the given output mode.
// If outputMode is empty, defaults to OutputAuto.
// jsonLogPath is only used in file mode.
func Init(levelStr string, jsonLogPath string) {
	InitWithMode(levelStr, jsonLogPath, OutputAuto)
}

// InitWithMode sets up logging with explicit output mode control.
func InitWithMode(levelStr string, jsonLogPath string, mode OutputMode) {
	level := parseLevel(levelStr)

	if mode == "" || mode == OutputAuto {
		mode = detectMode()
	}

	var handler slog.Handler
	switch mode {
	case OutputJournald:
		handler = initJournaldHandler(level)
	case OutputStdout:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default: // OutputFile
		handler = initCharmHandler(level, jsonLogPath)
	}

	slog.SetDefault(slog.New(handler))
}

// detectMode checks if we're running under systemd.
func detectMode() OutputMode {
	if os.Getenv("JOURNAL_STREAM") != "" {
		return OutputJournald
	}
	return OutputFile
}

// initJournaldHandler produces human-readable lines for systemd-journald.
// Journald prefixes its own timestamp, so we omit ours. No color (not a TTY).
func initJournaldHandler(level slog.Level) slog.Handler {
	handler := log.NewWithOptions(os.Stderr, log.Options{
		Level:           log.Level(level),
		ReportTimestamp: false,
	})
	styles := log.DefaultStyles()
	styles.Levels[log.Level(LevelTrace)] = lipgloss.NewStyle().SetString("TRAC")
	handler.SetStyles(styles)
	return handler
}

func initCharmHandler(level slog.Level, jsonLogPath string) slog.Handler {
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

	var slogHandler slog.Handler = handler
	if jsonLogPath != "" {
		f, err := os.OpenFile(jsonLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			slog.SetDefault(slog.New(handler))
			slog.Error("failed to open json log file", "path", jsonLogPath, "error", err)
			return handler
		}
		jsonHandler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
		slogHandler = &teeHandler{handlers: []slog.Handler{handler, jsonHandler}}
	}

	return slogHandler
}

// teeHandler fans out slog records to multiple handlers.
type teeHandler struct {
	handlers []slog.Handler
}

func (t *teeHandler) Enabled(_ context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(context.Background(), level) {
			return true
		}
	}
	return false
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &teeHandler{handlers: hs}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &teeHandler{handlers: hs}
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
