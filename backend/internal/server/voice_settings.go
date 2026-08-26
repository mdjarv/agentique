package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/voice"
)

// voiceSettingsHandler serves the operator's live-voice persona.
//
// These live in the database rather than config.toml because they are settings
// somebody changes to taste and wants to hear the effect of. A config value
// needs a restart, and a restart reaps every in-flight CLI process group —
// far too much to pay for trying a different voice.
type voiceSettingsHandler struct {
	queries *store.Queries
	// configModel is the [voice] model, shown as the placeholder so the page
	// says what "leave empty" actually means on this host.
	configModel string
	// previewOpts describes the engine an audition runs on — the same one a
	// real call uses, so what you hear is what you get.
	previewOpts voice.Options
	// previewing serialises auditions. Each one opens a short real session, so
	// a mashed button would otherwise open a dozen of them at once.
	previewing sync.Mutex
}

// wireVoiceSettings is the shape the settings page reads and writes.
//
// Every field is optional, per the wire rule: an older or newer client that
// omits one means "not set", not "clear it".
type wireVoiceSettings struct {
	VoiceName   string `json:"voiceName,omitempty"`
	Model       string `json:"model,omitempty"`
	Personality string `json:"personality,omitempty"`
	Verbosity   string `json:"verbosity,omitempty"`
	// ConfigModel is read-only: what a blank Model falls back to on this host.
	ConfigModel string `json:"configModel,omitempty"`
	// DefaultVoice is read-only: which voice a blank VoiceName actually uses.
	// The page can then mark that suggestion rather than saying "Default" and
	// leaving the operator to find out by calling.
	DefaultVoice string `json:"defaultVoice,omitempty"`
	// Voices are suggestions, not a closed set — the field accepts anything,
	// because the upstream list grows between agentique releases.
	Voices []wireVoiceOption `json:"voices,omitempty"`
	// Verbosities are the closed set, because this one *is* ours.
	Verbosities []wireVoiceOption `json:"verbosities,omitempty"`
}

type wireVoiceOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// suggestedVoices are Gemini's prebuilt voices as of writing.
//
// Suggestions only: the field is free text and nothing validates against this
// list, so a voice added upstream tomorrow is usable today by typing its name.
// Pinning an enum here would make a new voice need an agentique release.
var suggestedVoices = []wireVoiceOption{
	{Value: "", Label: "Default", Hint: "uses " + voice.DefaultVoiceName},
	{Value: "Puck", Label: "Puck", Hint: "bright, quick"},
	{Value: "Charon", Label: "Charon", Hint: "low, measured"},
	{Value: "Kore", Label: "Kore", Hint: "even, neutral"},
	{Value: "Fenrir", Label: "Fenrir", Hint: "warm, gravelly"},
	{Value: "Aoede", Label: "Aoede", Hint: "light, airy"},
	{Value: "Leda", Label: "Leda", Hint: "soft, close"},
	{Value: "Orus", Label: "Orus", Hint: "firm, clipped"},
	{Value: "Zephyr", Label: "Zephyr", Hint: "easy, unhurried"},
}

var verbosityOptions = []wireVoiceOption{
	{Value: string(voice.VerbosityBrief), Label: "Brief", Hint: "a sentence, sometimes two"},
	{Value: string(voice.VerbosityBalanced), Label: "Balanced", Hint: "room for a clause of context"},
	{Value: string(voice.VerbosityDetailed), Label: "Detailed", Hint: "explains its reasoning"},
}

// Persona implements voice.PersonaSource.
//
// A read failure returns the zero persona rather than an error: settings are a
// preference, and losing them should make a call plainer, never impossible.
func (h *voiceSettingsHandler) Persona(ctx context.Context) voice.Persona {
	row, err := h.queries.GetVoiceSettings(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("voice settings read failed", "error", err)
		}
		return voice.Persona{}
	}
	return voice.Persona{
		VoiceName:   row.VoiceName,
		Model:       row.Model,
		Personality: row.Personality,
		Verbosity:   voice.Verbosity(row.Verbosity),
	}
}

func (h *voiceSettingsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	persona := h.Persona(r.Context()).Sanitize()
	httperror.JSON(w, http.StatusOK, wireVoiceSettings{
		VoiceName:    persona.VoiceName,
		Model:        persona.Model,
		Personality:  persona.Personality,
		Verbosity:    string(persona.Verbosity),
		ConfigModel:  h.configModel,
		DefaultVoice: voice.DefaultVoiceName,
		Voices:       suggestedVoices,
		Verbosities:  verbosityOptions,
	})
}

func (h *voiceSettingsHandler) HandlePut(w http.ResponseWriter, r *http.Request) {
	var body wireVoiceSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body").WithCause(err))
		return
	}

	// Sanitised before storage, not just before use: the stored value is what a
	// later reader sees, and clamping once at the boundary means nothing
	// downstream has to remember to.
	persona := voice.Persona{
		VoiceName:   body.VoiceName,
		Model:       body.Model,
		Personality: body.Personality,
		Verbosity:   voice.Verbosity(body.Verbosity),
	}.Sanitize()

	err := h.queries.SetVoiceSettings(r.Context(), store.SetVoiceSettingsParams{
		VoiceName:   persona.VoiceName,
		Model:       persona.Model,
		Personality: persona.Personality,
		Verbosity:   string(persona.Verbosity),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		slog.Error("voice settings write failed", "error", err)
		httperror.RespondError(w, httperror.Internal("could not save voice settings", err))
		return
	}

	// Answer with what was stored, so the page shows the clamped value rather
	// than the text the user typed.
	h.HandleGet(w, r)
}

// HandlePreview synthesises the preview line in one voice and returns WAV.
//
// The audition runs through the same engine a call uses, so it is the thing
// itself rather than an approximation from a different endpoint that might not
// match. It costs a short real session, which is why it is one sentence and
// why concurrent clicks are serialised.
func (h *voiceSettingsHandler) HandlePreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VoiceName string `json:"voiceName,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body").WithCause(err))
		return
	}

	// One at a time. Auditioning is a button people click repeatedly while
	// comparing, and each click is a paid session.
	h.previewing.Lock()
	defer h.previewing.Unlock()

	wav, err := voice.Preview(r.Context(), h.previewOpts, body.VoiceName)
	if errors.Is(err, voice.ErrPreviewUnsupported) {
		httperror.RespondError(w, httperror.BadRequest(
			"this machine is set to the loopback echo backend, which has no voice to preview"))
		return
	}
	if err != nil {
		// The detail goes to the log; the response says what a person can act
		// on, which is usually "that voice name is not one it knows".
		slog.Warn("voice preview failed", "voice", body.VoiceName, "error", err)
		httperror.RespondError(w, httperror.BadRequest(
			"could not preview that voice — check the name is one the backend knows"))
		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
	_, _ = w.Write(wav)
}
