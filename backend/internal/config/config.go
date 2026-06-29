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

	"github.com/BurntSushi/toml"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

type Config struct {
	Server       ServerConfig       `toml:"server"`
	Logging      LoggingConfig      `toml:"logging"`
	Backup       BackupConfig       `toml:"backup"`
	Setup        SetupConfig        `toml:"setup"`
	Experimental ExperimentalConfig `toml:"experimental"`
	Brain        BrainConfig        `toml:"brain"`
	DevURLs      []DevURLSlot       `toml:"dev-urls"`
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
}

type LoggingConfig struct {
	Level  string `toml:"level"`
	Output string `toml:"output"` // auto, journald, file, stdout
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

	// --- Semantic recall (the embedder + vector DB). All optional; when ChromaURL,
	// EmbedURL and EmbedModel are all set and Chroma answers a heartbeat, recall becomes
	// hybrid (keyword + embedding cosine). Each has an AGENTIQUE_BRAIN_* env override that
	// wins when set. See docs/brain-semantic-recall.md.

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
func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// Exists reports whether a config file is present at the default path.
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}
