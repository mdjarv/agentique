package voice

import (
	"io"
	"log/slog"
)

// testLogger keeps test output quiet without leaving a nil *slog.Logger to
// panic on the first call.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
