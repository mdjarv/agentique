package session

import "github.com/allbin/agentkit/runtime"

// WireCapabilities is the wire-facing snapshot of a CLI provider's feature
// flags. It mirrors runtime.Capabilities flatly so the frontend can dispatch
// UI affordances (resume, mid-turn send, plan mode, etc.) without needing a
// live session — the values are a deterministic function of the persisted
// provider name and the linked agentkit adapter version.
//
// Source of truth: runtime.Capabilities. If the adapter side ever advertises
// values that differ from the static lookup here, the frontend will gate based
// on the static profile; update capabilitiesForProvider when bumping agentkit
// to keep them in sync.
type WireCapabilities struct {
	Provider               string `json:"provider"`
	ProviderVersion        string `json:"providerVersion,omitempty"`
	PlanMode               bool   `json:"planMode"`
	AcceptEditsMode        bool   `json:"acceptEditsMode"`
	Effort                 bool   `json:"effort"`
	MaxBudget              bool   `json:"maxBudget"`
	MaxTurns               bool   `json:"maxTurns"`
	Thinking               bool   `json:"thinking"`
	PartialMessageStream   bool   `json:"partialMessageStream"`
	Subagents              bool   `json:"subagents"`
	RateLimitEvents        bool   `json:"rateLimitEvents"`
	CompactionEvents       bool   `json:"compactionEvents"`
	InteractivePermissions bool   `json:"interactivePermissions"`
	AskUserQuestion        bool   `json:"askUserQuestion"`
	GranularPermissions    bool   `json:"granularPermissions"`
	SandboxModes           bool   `json:"sandboxModes"`
	Resume                 bool   `json:"resume"`
	Fork                   bool   `json:"fork"`
	MidTurnSendMessage     bool   `json:"midTurnSendMessage"`
	Ping                   bool   `json:"ping"`
	ToolProgressTicks      bool   `json:"toolProgressTicks"`
	Attachments            bool   `json:"attachments"`
	// ModelSwitch indicates the adapter implements runtime.ModelSwitchable.
	// Codex's adapter currently returns ErrNotSupported, so the UI keeps the
	// model picker read-only for codex sessions.
	ModelSwitch bool `json:"modelSwitch"`
}

// runtimeCapsToWire flattens a runtime.Capabilities into the wire shape. The
// Attachments flag is not part of the upstream runtime.Capabilities struct;
// it's derived per provider (claude supports image/document attachments;
// codex's userInput rejects them — see docs/tech-debt.md "Codex attachments").
func runtimeCapsToWire(c runtime.Capabilities, attachments bool) WireCapabilities {
	return WireCapabilities{
		Provider:               c.Provider,
		ProviderVersion:        c.ProviderVersion,
		PlanMode:               c.PlanMode,
		AcceptEditsMode:        c.AcceptEditsMode,
		Effort:                 c.Effort,
		MaxBudget:              c.MaxBudget,
		MaxTurns:               c.MaxTurns,
		Thinking:               c.Thinking,
		PartialMessageStream:   c.PartialMessageStream,
		Subagents:              c.Subagents,
		RateLimitEvents:        c.RateLimitEvents,
		CompactionEvents:       c.CompactionEvents,
		InteractivePermissions: c.InteractivePermissions,
		AskUserQuestion:        c.AskUserQuestion,
		GranularPermissions:    c.GranularPermissions,
		SandboxModes:           c.SandboxModes,
		Resume:                 c.Resume,
		Fork:                   c.Fork,
		MidTurnSendMessage:     c.MidTurnSendMessage,
		Ping:                   c.Ping,
		ToolProgressTicks:      c.ToolProgressTicks,
		Attachments:            attachments,
	}
}

// capabilitiesForProvider returns the static capability snapshot for a
// canonical provider name. Unknown providers get a zero value with just the
// name populated, which the UI treats as "nothing is supported" — safer than
// silently advertising claude's full feature set.
func capabilitiesForProvider(provider string) WireCapabilities {
	switch normalizeProvider(provider) {
	case "claude":
		return WireCapabilities{
			Provider:               "claude",
			PlanMode:               true,
			AcceptEditsMode:        true,
			Effort:                 true,
			MaxBudget:              true,
			MaxTurns:               true,
			Thinking:               true,
			PartialMessageStream:   true,
			Subagents:              true,
			RateLimitEvents:        true,
			CompactionEvents:       true,
			InteractivePermissions: true,
			AskUserQuestion:        true,
			Resume:                 true,
			Fork:                   true,
			MidTurnSendMessage:     true,
			Ping:                   true,
			ToolProgressTicks:      true,
			Attachments:            true,
			ModelSwitch:            true,
		}
	case "codex":
		return WireCapabilities{
			Provider:               "codex",
			Effort:                 true,
			PartialMessageStream:   true,
			InteractivePermissions: true,
			AskUserQuestion:        true,
			GranularPermissions:    true,
			SandboxModes:           true,
			Resume:                 true,
			RateLimitEvents:        true,
			Ping:                   true,
		}
	default:
		return WireCapabilities{Provider: provider}
	}
}
