// Package config handles file-based configuration for Agentique.
//
// Config is loaded from <config-dir>/config.toml (~/.config/agentique/ on Linux).
// CLI flags take precedence over config file values.
// Missing config file is not an error — defaults apply.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

type Config struct {
	Server       ServerConfig       `toml:"server"`
	Session      SessionConfig      `toml:"session"`
	Scheduler    SchedulerConfig    `toml:"scheduler"`
	Logging      LoggingConfig      `toml:"logging"`
	Update       UpdateConfig       `toml:"update"`
	Backup       BackupConfig       `toml:"backup"`
	Setup        SetupConfig        `toml:"setup"`
	Experimental ExperimentalConfig `toml:"experimental"`
	Claude       ClaudeConfig       `toml:"claude"`
	Brain        BrainConfig        `toml:"brain"`
	Voice        VoiceConfig        `toml:"voice"`
	DevURLs      []DevURLSlot       `toml:"dev-urls"`
	// Models overrides the auto-detected model catalog, keyed by provider
	// ("claude", "codex"). A non-empty list replaces that provider's generated
	// list entirely — it is the escape hatch for anything auto-detection misses.
	//
	//	[[models.claude]]
	//	slug = "opus"
	//	display = "Opus 5"
	Models map[string][]ModelOverride `toml:"models"`
}

// ModelOverride is one config-supplied entry in the model picker.
type ModelOverride struct {
	// Slug is passed to the provider CLI verbatim (an alias like "opus" or a
	// pinned ID like "claude-opus-5"). Required; entries without it are dropped.
	Slug string `toml:"slug"`
	// Display is the picker label. Defaults to Slug when empty.
	Display string `toml:"display"`
	// Description is optional secondary text in the picker.
	Description string `toml:"description"`
}

// SessionConfig tunes session lifecycle behavior.
type SessionConfig struct {
	// IdleEvictTimeout, when set to a positive duration (e.g. "30m"), stops a
	// session that has been idle at least this long to reclaim its CLI process
	// and Playwright/Chrome subtree. The session resumes transparently on the
	// next message. "" (the default) disables idle eviction. Env override:
	// AGENTIQUE_SESSION_IDLE_EVICT_TIMEOUT.
	IdleEvictTimeout string `toml:"idle-evict-timeout"`
}

// SchedulerConfig tunes scheduled loops (docs/scheduled-loops.md). Durations
// are Go duration strings; empty fields fall back to the defaults noted per
// field. Env overrides follow AGENTIQUE_SCHEDULER_<KEY>.
type SchedulerConfig struct {
	// Disabled turns the scheduler off entirely (schedules persist but never
	// fire). Env: AGENTIQUE_SCHEDULER_DISABLED.
	Disabled bool `toml:"disabled"`
	// TickInterval is the due-schedule poll cadence. Default 20s.
	TickInterval string `toml:"tick-interval"`
	// MinInterval is the hard floor for cron cadence and dynamic delays.
	// Default 1m.
	MinInterval string `toml:"min-interval"`
	// MaxRunDuration marks a running fire overdue (attention, not error)
	// after this long. Default 30m.
	MaxRunDuration string `toml:"max-run-duration"`
	// MaxConsecutiveFailures auto-pauses a schedule after this many error
	// terminals in a row. Default 3.
	MaxConsecutiveFailures int `toml:"max-consecutive-failures"`
	// RunHistory is the retained runs per schedule (pruned by creation
	// order). Default 200.
	RunHistory int `toml:"run-history"`
	// OnceCatchupWindow bounds how stale a missed one-shot may fire.
	// Default 1h.
	OnceCatchupWindow string `toml:"once-catchup-window"`
	// DynamicMaxDelay clamps ScheduleNext delays. Default 6h.
	DynamicMaxDelay string `toml:"dynamic-max-delay"`
	// DynamicFallback is the pre-written next fire a dynamic run gets in
	// case it never reschedules. Default 20m.
	DynamicFallback string `toml:"dynamic-fallback"`
}

// DevURLSlot describes one publicly-routable dev frontend URL.
// Sessions can lease a slot to expose a Vite dev server externally.
type DevURLSlot struct {
	Slot       string `toml:"slot"`
	Port       int    `toml:"port"`
	PublicHost string `toml:"public-host"`
}

// AllRPOrigins returns every origin the WebAuthn RP allowlist should accept:
// the primary Server.RPOrigin plus "https://<public-host>" for each configured
// dev-url slot. Empty origins are skipped and duplicates are removed, preserving
// first-seen order (primary origin first).
func (c *Config) AllRPOrigins() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(o string) {
		if o == "" || seen[o] {
			return
		}
		seen[o] = true
		out = append(out, o)
	}
	add(c.Server.RPOrigin)
	for _, s := range c.DevURLs {
		if s.PublicHost == "" {
			continue
		}
		add("https://" + s.PublicHost)
	}
	return out
}

// ValidateDevURLs checks that slots have non-empty fields, valid port ranges,
// and unique (slot, port, public-host) tuples.
func ValidateDevURLs(slots []DevURLSlot) error {
	seenSlot := map[string]bool{}
	seenPort := map[int]bool{}
	seenHost := map[string]bool{}
	for i, s := range slots {
		if s.Slot == "" {
			return fmt.Errorf("dev-urls[%d]: slot name is required", i)
		}
		if s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("dev-urls[%d] (%s): port must be 1-65535, got %d", i, s.Slot, s.Port)
		}
		if s.PublicHost == "" {
			return fmt.Errorf("dev-urls[%d] (%s): public-host is required", i, s.Slot)
		}
		if seenSlot[s.Slot] {
			return fmt.Errorf("dev-urls: duplicate slot name %q", s.Slot)
		}
		if seenPort[s.Port] {
			return fmt.Errorf("dev-urls: duplicate port %d", s.Port)
		}
		if seenHost[s.PublicHost] {
			return fmt.Errorf("dev-urls: duplicate public-host %q", s.PublicHost)
		}
		seenSlot[s.Slot] = true
		seenPort[s.Port] = true
		seenHost[s.PublicHost] = true
	}
	return nil
}

type ExperimentalConfig struct {
	Teams   bool `toml:"teams"`
	Browser bool `toml:"browser"`
	// Voice enables the live spoken-dialog composer mode. The socket endpoint
	// and its browser affordance exist only when this is on; talking to a
	// speech model additionally needs credentials in [voice].
	Voice bool `toml:"voice"`
}

// ClaudeConfig carries flags handed to the claude CLI when a session's provider
// is "claude". They are connector-wide defaults, not per-session settings.
// Env overrides follow AGENTIQUE_CLAUDE_<KEY>.
//
// Deliberately absent: --safe-mode (it disables MCP servers, and agentique's own
// tools reach sessions over MCP, so a safe-mode session loses SendMessage,
// memory, dev URLs and the browser) and --plugin-url (agentique has no plugin
// story to point it at).
type ClaudeConfig struct {
	// ExcludeDynamicSystemPromptSections moves the per-machine system-prompt
	// sections (cwd, env info, memory paths, git status) into the first user
	// message, so the cached prefix ahead of agentique's own appended preamble
	// is shared between sessions instead of diverging at each worktree's path
	// and git status. Off by default: it changes prompt structure, and the win
	// is bounded by how much sits between those sections and the append point.
	// Env: AGENTIQUE_CLAUDE_EXCLUDE_DYNAMIC_SYSTEM_PROMPT_SECTIONS.
	ExcludeDynamicSystemPromptSections bool `toml:"exclude-dynamic-system-prompt-sections"`
	// AutoCompact sets the auto-compact window: "auto" to let the CLI choose,
	// or a token count between 100000 and 1000000. "" (the default) leaves the
	// CLI's own behavior alone. Env: AGENTIQUE_CLAUDE_AUTOCOMPACT.
	AutoCompact string `toml:"autocompact"`
	// ForwardSubagentText surfaces what subagents say, not just that they ran:
	// the CLI forwards their text and thinking as ordinary events carrying
	// ParentToolUseID, and the frontend nests those under the spawning Task
	// block. Off by default because it is a real increase in event volume on
	// subagent-heavy turns. Env: AGENTIQUE_CLAUDE_FORWARD_SUBAGENT_TEXT.
	ForwardSubagentText bool `toml:"forward-subagent-text"`
}

// AutoCompactMin and AutoCompactMax bound the --autocompact token window the
// CLI accepts.
const (
	AutoCompactMin = 100_000
	AutoCompactMax = 1_000_000
)

// Validate reports a malformed [claude] section. Callers should treat this as
// fatal at startup: the CLI rejects a bad --autocompact at spawn time, where
// the failure surfaces as a session that dies instead of a config error.
func (c ClaudeConfig) Validate() error {
	if c.AutoCompact == "" || c.AutoCompact == "auto" {
		return nil
	}
	n, err := strconv.Atoi(c.AutoCompact)
	if err != nil {
		return fmt.Errorf("claude autocompact %q: must be \"auto\" or a token count", c.AutoCompact)
	}
	if n < AutoCompactMin || n > AutoCompactMax {
		return fmt.Errorf("claude autocompact %d: must be between %d and %d", n, AutoCompactMin, AutoCompactMax)
	}
	return nil
}

type SetupConfig struct {
	InitialProject string `toml:"initial-project"` // path to auto-create on first serve
}

type ServerConfig struct {
	Addr        string `toml:"addr"`
	DisableAuth bool   `toml:"disable-auth"`
	TLSCert     string `toml:"tls-cert"`
	TLSKey      string `toml:"tls-key"`
	RPID        string `toml:"rp-id"`
	RPOrigin    string `toml:"rp-origin"`
	// MachineLabel overrides the machine name shown to multi-machine clients
	// (default: PRETTY_HOSTNAME, else the OS hostname). Env override:
	// AGENTIQUE_MACHINE_LABEL.
	MachineLabel string `toml:"machine-label"`
}

type LoggingConfig struct {
	Level  string `toml:"level"`
	Output string `toml:"output"` // auto, journald, file, stdout
}

// UpdateConfig tunes the in-app upgrade check (docs/upgrades.md). Each field
// has an AGENTIQUE_UPDATE_* env override which wins over the file value.
type UpdateConfig struct {
	// APIURL overrides the GitHub "latest release" endpoint — a fork's repo,
	// or a stub server when verifying the apply path against throwaway
	// servers. Empty uses the upstream agentique repo. Env:
	// AGENTIQUE_UPDATE_API_URL.
	APIURL string `toml:"api-url"`
	// Interval is the background check period (e.g. "1h"); empty uses the
	// default hour. Env: AGENTIQUE_UPDATE_INTERVAL.
	Interval string `toml:"interval"`
	// Disabled turns the check off entirely: no polling, and
	// /api/update/status reports the current version with no latest.
	// Env: AGENTIQUE_UPDATE_DISABLED.
	Disabled bool `toml:"disabled"`
	// ArmDeadline bounds how long an upgrade armed for the next idle boundary
	// waits before giving up and saying so (e.g. "4h"); empty takes the
	// default. Env: AGENTIQUE_UPDATE_ARM_DEADLINE.
	ArmDeadline string `toml:"arm-deadline"`
}

type BackupConfig struct {
	Interval string `toml:"interval"`
	Retain   int    `toml:"retain"`
	Disabled bool   `toml:"disabled"`
}

// BrainConfig configures the persistent agent memory ("brain"). Each field has an
// equivalent AGENTIQUE_BRAIN_* env var which, when set, takes precedence over the file
// value (env is the runtime override; the file is the persistent default). An empty value
// means "unset" — the corresponding feature stays off / uses its built-in default, exactly
// as when no env var is set.
type BrainConfig struct {
	// ConsolidateInterval enables scheduled (automatic) consolidation across all scopes
	// when set to a positive duration (e.g. "6h"); empty disables it. Env:
	// AGENTIQUE_BRAIN_CONSOLIDATE_INTERVAL.
	ConsolidateInterval string `toml:"consolidate-interval"`
	// ConsolidateModel is the model the scheduled consolidation uses for LLM
	// reorganization (haiku|sonnet|opus). Empty = deterministic dedup/decay only.
	// Env: AGENTIQUE_BRAIN_CONSOLIDATE_MODEL.
	ConsolidateModel string `toml:"consolidate-model"`
	// LearnModel enables session-end auto-encode — distilling durable facts from a
	// finished session's transcript when it is deleted (haiku|sonnet|opus). Empty = off.
	// Env: AGENTIQUE_BRAIN_LEARN_MODEL.
	LearnModel string `toml:"learn-model"`
	// OutcomeModel enables the session-end automatic outcome emitter — an LLM judge over
	// the finished transcript that decides whether the facts recall surfaced during the
	// session helped (→ strengthen) or were contradicted (→ flag for review), feeding the
	// outcome signal automatically instead of relying on agents to call MemoryUsed/MemoryFlag
	// (haiku|sonnet|opus). Empty = off. Env: AGENTIQUE_BRAIN_OUTCOME_MODEL.
	OutcomeModel string `toml:"outcome-model"`

	// SnapshotRetain bounds how many pre-churn brain snapshots are kept under
	// brain/.snapshots/. 0 = the built-in default (7); do not duplicate that default here.
	// Env: AGENTIQUE_BRAIN_SNAPSHOT_RETAIN.
	SnapshotRetain int `toml:"snapshot-retain"`

	// ArchiveAfter enables disuse-aging archival when set to a positive duration (e.g.
	// "720h" = 30 days): the hard minimum a fact must go untouched before the churn archives
	// it once its effective confidence has faded below the floor. "" (the default) = OFF — no
	// recall fade-out, no archive (preserves today's behaviour until an operator opts in after
	// curating). Env: AGENTIQUE_BRAIN_ARCHIVE_AFTER.
	ArchiveAfter string `toml:"archive-after"`
	// ArchiveConfidenceFloor is the effective-confidence line below which a faded fact is
	// archived/faded from recall. 0 = the built-in default (0.35). Env: AGENTIQUE_BRAIN_ARCHIVE_FLOOR.
	ArchiveConfidenceFloor float64 `toml:"archive-confidence-floor"`

	// RetryMax bounds how many times a session-end learn/outcome job is retried before it is
	// dead-lettered. 0 = the built-in default (5). Env: AGENTIQUE_BRAIN_RETRY_MAX.
	RetryMax int `toml:"retry-max"`

	// --- Semantic recall (the embedder + vector DB). All optional; when ChromaURL,
	// EmbedURL and EmbedModel are all set and Chroma answers a heartbeat, recall becomes
	// hybrid (keyword + embedding cosine). Each has an AGENTIQUE_BRAIN_* env override that
	// wins when set. See docs/brain.md#semantic-recall.

	// ChromaURL is the Chroma (vector DB) base URL, e.g. http://127.0.0.1:8000.
	// Env: AGENTIQUE_BRAIN_CHROMA_URL.
	ChromaURL string `toml:"chroma-url"`
	// EmbedURL is the OpenAI-compatible embeddings endpoint, e.g.
	// http://127.0.0.1:11434/v1/embeddings (Ollama). Env: AGENTIQUE_BRAIN_EMBED_URL.
	EmbedURL string `toml:"embed-url"`
	// EmbedModel is the embedding model id, e.g. all-minilm. Env: AGENTIQUE_BRAIN_EMBED_MODEL.
	EmbedModel string `toml:"embed-model"`
	// EmbedKey is an optional API key for the embeddings endpoint (unset for a local
	// Ollama). Env: AGENTIQUE_BRAIN_EMBED_KEY.
	EmbedKey string `toml:"embed-key"`
	// SemanticThreshold overrides the cosine "related" link/vouch threshold (model-specific;
	// 0 = built-in default 0.45). Inert without an embedder. Env: AGENTIQUE_BRAIN_SEMANTIC_THRESHOLD.
	SemanticThreshold float64 `toml:"semantic-threshold"`
	// VectorVeto overrides the hybrid-recall vector veto floor (model-specific; 0 = built-in
	// default 0.15). Inert without an embedder. Env: AGENTIQUE_BRAIN_VECTOR_VETO.
	VectorVeto float64 `toml:"vector-veto"`
	// Autocal derives the semantic thresholds from the live corpus's own cosine distribution
	// at boot instead of the hand-set defaults (model-specific). An explicitly-set
	// SemanticThreshold/VectorVeto still wins. Inert without an embedder.
	// Env: AGENTIQUE_BRAIN_AUTOCAL.
	Autocal bool `toml:"autocal"`

	// Recall toggles auto-recall (pinned facts + per-turn task-relevant facts injected into
	// the preamble). It is ON by default; set to "off" (or false/0/no) to disable. Empty =
	// default on. Env: AGENTIQUE_BRAIN_RECALL (wins when set).
	Recall string `toml:"recall"`

	// Graph tunes the brain knowledge-graph view: the semantic kNN edge density computed on
	// the backend and the force-layout curves sent to the frontend. All optional; any field
	// left 0 keeps the built-in default. See [brain.graph] in config.toml. Each field has an
	// AGENTIQUE_BRAIN_GRAPH_* env override that wins when set.
	Graph BrainGraphConfig `toml:"graph"`
}

// VoiceConfig configures the live spoken-dialog mode ("Live"). Like [brain], every
// field has an equivalent AGENTIQUE_VOICE_* env var which takes precedence when set,
// and an empty value means "unset" rather than a hardcoded default.
//
// The feature is gated by [experimental] voice; this section only says which speech
// backend to talk to. With the flag on and no credentials here, the socket still
// serves the loopback echo used to verify audio plumbing, and no model is contacted.
type VoiceConfig struct {
	// Backend selects the realtime speech transport: "aistudio" (an API key from
	// aistudio.google.com) or "vertex" (a Google Cloud project with application
	// default credentials). Empty = aistudio.
	//
	// The two differ in credentials and data terms, not in protocol — the same SDK
	// and the same Live session config drive both — so switching is a config change
	// rather than a rewrite. Vertex is the better fit for work accounts: enterprise
	// data terms, IAM and audit logging come with the project.
	// Env: AGENTIQUE_VOICE_BACKEND.
	Backend string `toml:"backend"`

	// APIKey authenticates the aistudio backend. Ignored by vertex.
	//
	// Note the tier matters beyond rate limits: free-tier content may be used to
	// improve Google's products, paid-tier content may not. Env: AGENTIQUE_VOICE_API_KEY.
	APIKey string `toml:"api-key"`

	// Project is the Google Cloud project id for the vertex backend. Ignored by
	// aistudio. Env: AGENTIQUE_VOICE_PROJECT.
	Project string `toml:"project"`
	// Location is the Vertex region, e.g. "us-central1". Ignored by aistudio.
	// Env: AGENTIQUE_VOICE_LOCATION.
	Location string `toml:"location"`

	// Model is the realtime speech model id. Empty = the backend's built-in default.
	//
	// It is configuration rather than a constant for the same reason the model catalog
	// keeps versions out of picker labels: a new upstream model must not require an
	// agentique release. The two backends do not carry identical model ids, so this
	// changes with Backend. Env: AGENTIQUE_VOICE_MODEL.
	Model string `toml:"model"`

	// IdleTimeout closes a call whose microphone has been open with no speech for
	// this long (e.g. "90s"). Empty = the built-in default.
	//
	// This one is not a nicety. A live session bills for wall-clock time with the
	// microphone open, so unlike everything else in agentique an abandoned tab keeps
	// costing until something closes it. Env: AGENTIQUE_VOICE_IDLE_TIMEOUT.
	IdleTimeout string `toml:"idle-timeout"`
}

// BrainGraphConfig tunes the brain knowledge-graph view. The two edge fields shape the
// backend semantic kNN (how dense the graph is); the force-layout fields are passed through
// to the frontend on the graph payload so the layout's geometry is tunable per deployment
// without a rebuild. Every field is optional — a 0 value means "use the built-in default"
// (the brain package owns those defaults, so they live in exactly one place).
type BrainGraphConfig struct {
	// EdgeCap bounds how many nearest-neighbour edges each fact contributes to the graph, so
	// a densely-related cluster doesn't become a hairball (0 = default 6). The union of
	// asymmetric kNN can still push a popular node a little over this.
	// Env: AGENTIQUE_BRAIN_GRAPH_EDGE_CAP.
	EdgeCap int `toml:"edge-cap"`
	// EdgeThreshold is the cosine floor a pair must clear to become a graph edge — raise it
	// for a sparser graph, lower it for a denser one (0 = the recall semantic-threshold).
	// Env: AGENTIQUE_BRAIN_GRAPH_EDGE_THRESHOLD.
	EdgeThreshold float64 `toml:"edge-threshold"`
	// LinkStrengthBase is the force-layout link strength at association weight 0 (the weakest
	// drawn edge); 0 = default 0.04. Env: AGENTIQUE_BRAIN_GRAPH_LINK_STRENGTH_BASE.
	LinkStrengthBase float64 `toml:"link-strength-base"`
	// LinkStrengthSpan is added to LinkStrengthBase at weight 1 (strongest edge), so a strong
	// association pulls harder; 0 = default 0.32. Env: AGENTIQUE_BRAIN_GRAPH_LINK_STRENGTH_SPAN.
	LinkStrengthSpan float64 `toml:"link-strength-span"`
	// LinkDistanceBase is the force-layout link distance at weight 0; 0 = default 90.
	// Env: AGENTIQUE_BRAIN_GRAPH_LINK_DISTANCE_BASE.
	LinkDistanceBase float64 `toml:"link-distance-base"`
	// LinkDistanceSpan is subtracted from LinkDistanceBase at weight 1, so a strong
	// association sits closer; 0 = default 55. Env: AGENTIQUE_BRAIN_GRAPH_LINK_DISTANCE_SPAN.
	LinkDistanceSpan float64 `toml:"link-distance-span"`
	// Gravity is the radial pull toward the origin that keeps isolated facts from flinging
	// out under charge repulsion; 0 = default 0.045. Env: AGENTIQUE_BRAIN_GRAPH_GRAVITY.
	Gravity float64 `toml:"gravity"`
}

// Default returns a config with all default values.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: "localhost:9201",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Output: "auto",
		},
		Backup: BackupConfig{
			Interval: "15m",
			Retain:   7,
		},
	}
}

// Path returns the default config file location.
func Path() string {
	return filepath.Join(paths.ConfigDir(), "config.toml")
}

// Load reads config from the given path. Returns defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes config to the given path, creating parent directories as needed.
//
// Owner-only: this file carries the embeddings API key and the TLS key path,
// and a group-writable config is worse than a readable one — anyone in the
// group could point the server at their own listen address or brain backend.
func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	// An existing file keeps its old mode through O_CREATE, so fix it forward.
	return os.Chmod(path, 0o600)
}

// Exists reports whether a config file is present at the default path.
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}
