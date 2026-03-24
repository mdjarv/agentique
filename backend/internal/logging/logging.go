package logging

import (
	"log/slog"
	"os"

	"github.com/charmbracelet/log"
)

// Init sets up charmbracelet/log as the default slog handler with colored output.
func Init() {
	handler := log.NewWithOptions(os.Stderr, log.Options{
		Level:           log.DebugLevel,
		ReportTimestamp: true,
	})
	slog.SetDefault(slog.New(handler))
}
