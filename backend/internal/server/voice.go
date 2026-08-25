package server

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/mdjarv/agentique/backend/internal/voice"
)

// newVoiceHandler builds the live voice socket handler from [voice] config.
//
// Credentials are optional. A configured backend missing its credential
// degrades to the loopback echo engine rather than failing: the audio path is
// worth exercising on its own, and the alternative — refusing to mount the
// route — makes a plumbing problem look like a missing feature. The degrade is
// logged at warn, because silently answering with an echo when someone expected
// a model would be worse than either.
func newVoiceHandler(cfg Config, allowedOrigins map[string]bool) (*voice.Handler, error) {
	backend, err := voice.ParseBackend(cfg.Voice.Backend)
	if err != nil {
		return nil, fmt.Errorf("resolve voice backend: %w", err)
	}

	switch backend {
	case voice.BackendAIStudio:
		if cfg.Voice.APIKey == "" {
			slog.Warn("live voice: no [voice] api-key, falling back to the loopback echo engine")
			backend = voice.BackendEcho
		}
	case voice.BackendVertex:
		if cfg.Voice.Project == "" {
			slog.Warn("live voice: no [voice] project, falling back to the loopback echo engine")
			backend = voice.BackendEcho
		}
	}

	var idleTimeout time.Duration
	if raw := cfg.Voice.IdleTimeout; raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse [voice] idle-timeout %q: %w", raw, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("[voice] idle-timeout %q must be positive", raw)
		}
		idleTimeout = d
	}

	return voice.NewHandler(voice.Options{
		Backend:           backend,
		APIKey:            cfg.Voice.APIKey,
		Project:           cfg.Voice.Project,
		Location:          cfg.Voice.Location,
		Model:             cfg.Voice.Model,
		IdleTimeout:       idleTimeout,
		AllowedOrigins:    allowedOrigins,
		AllowTicketOrigin: cfg.AuthEnabled,
	})
}
