package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// claudeAliases is the one-choice-per-family set shown in the picker. Sonnet
// and Opus use the CLI's stable 1M aliases; the context size is an execution
// detail, so their labels stay as bare family names.
var claudeAliases = []string{
	"haiku",
	"sonnet[1m]",
	"opus[1m]",
	"fable",
}

// oneMSuffix marks a 1M-context variant of an alias.
const oneMSuffix = "[1m]"

func (c *Catalog) claudeModels(resolved map[string]string) ProviderModels {
	if override, ok := c.applyOverride("claude"); ok {
		return override
	}

	models := make([]ModelInfo, 0, len(claudeAliases)+2)
	seenSlug := map[string]bool{}
	seenResolved := map[string]bool{}
	seenFamily := map[string]bool{}
	learned := false

	for _, slug := range claudeAliases {
		id := resolved[slug]
		if id != "" {
			learned = true
			seenResolved[normalizeModelID(id)] = true
		}
		models = append(models, ModelInfo{
			Slug:        slug,
			DisplayName: claudeLabel(slug),
			ResolvedID:  id,
		})
		seenSlug[slug] = true
		seenFamily[strings.ToLower(claudeLabel(slug))] = true
	}

	// Models the CLI itself advertises beyond the built-in aliases (promotional
	// tiers, extra context-window variants). This is how a genuinely new family
	// reaches the picker without an agentique release.
	for _, extra := range cliModelOptions(c.claudeConfigPath()) {
		family := strings.ToLower(extra.DisplayName)
		if seenSlug[extra.Slug] || seenResolved[normalizeModelID(extra.Slug)] || seenFamily[family] {
			continue
		}
		seenSlug[extra.Slug] = true
		seenFamily[family] = true
		learned = true
		models = append(models, extra)
	}

	source := "static"
	if learned {
		source = "learned"
	}
	return ProviderModels{Provider: "claude", Source: source, Models: models}
}

// claudeLabel renders the stable family name used in the picker. The concrete
// version belongs to the session that reported it, not to this global list.
func claudeLabel(slug string) string {
	return ModelFamilyName(strings.TrimSuffix(slug, oneMSuffix))
}

// ModelFamilyName extracts the stable family name from a Claude model ID:
// "claude-opus-5" -> "Opus", "claude-3-5-sonnet-20241022" -> "Sonnet".
func ModelFamilyName(id string) string {
	family, _ := modelNameParts(id)
	return family
}

// ModelDisplayName renders a human-readable name from a Claude model ID:
// "claude-opus-5" -> "Opus 5", "claude-3-5-sonnet-20241022" -> "Sonnet 3.5",
// "opus" -> "Opus". The name is parsed from the ID's structure rather than a
// lookup table, so an unreleased family renders correctly with no code change.
//
// This generalizes claudecli.ModelDisplayName, which only recognizes the
// opus/sonnet/haiku tiers and therefore returns "claude-fable-5" verbatim.
// A bracketed context marker ("[1m]") and 8-digit date stamps are ignored; an
// ID with no family token is returned unchanged.
func ModelDisplayName(id string) string {
	name, nums := modelNameParts(id)
	if name == "" {
		return normalizeModelID(id)
	}
	if len(nums) > 0 {
		name += " " + strings.Join(nums, ".")
	}
	return name
}

func modelNameParts(id string) (string, []string) {
	id = normalizeModelID(id)
	if id == "" {
		return "", nil
	}

	family := ""
	var nums []string
	for _, tok := range strings.Split(id, "-") {
		switch {
		case tok == "" || tok == "claude":
			continue
		case isAllDigits(tok):
			if len(tok) != 8 { // skip YYYYMMDD date stamps
				nums = append(nums, tok)
			}
		case family == "":
			family = tok
		}
	}
	if family == "" {
		return "", nil
	}

	name := strings.ToUpper(family[:1]) + family[1:]
	return name, nums
}

// normalizeModelID lowercases and strips a bracketed context marker so that
// "claude-opus-5[1m]" and "claude-opus-5" compare equal.
func normalizeModelID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.IndexByte(id, '['); i >= 0 {
		id = strings.TrimSpace(id[:i])
	}
	return id
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// claudeConfigPath resolves the Claude CLI's config JSON, honoring
// CLAUDE_CONFIG_DIR the same way the CLI does.
func (c *Catalog) claudeConfigPath() string {
	if c.cliOptionsPath != "" {
		return c.cliOptionsPath
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// claudeCLIConfig is the sliver of ~/.claude.json we read. The CLI writes
// additionalModelOptionsCache when the account has access to models beyond the
// built-in set; a missing file or key simply yields no extras.
type claudeCLIConfig struct {
	AdditionalModelOptions []struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"additionalModelOptionsCache"`
}

// cliModelOptions reads the CLI's advertised extra models. Every failure mode
// (no file, unreadable, malformed) degrades to "no extras" — this layer is
// enrichment, never a precondition for serving a catalog.
func cliModelOptions(path string) []ModelInfo {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is the user's own CLI config
	if err != nil {
		return nil
	}
	var cfg claudeCLIConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}

	out := make([]ModelInfo, 0, len(cfg.AdditionalModelOptions))
	for _, opt := range cfg.AdditionalModelOptions {
		if opt.Value == "" {
			continue
		}
		// Keep the global picker on stable family names. The exact version is
		// shown only on a session after the provider reports what it ran.
		name := ModelFamilyName(opt.Value)
		if name == "" {
			name = opt.Label
		}
		out = append(out, ModelInfo{
			Slug:        opt.Value,
			DisplayName: name,
			Description: opt.Description,
			ResolvedID:  opt.Value,
		})
	}
	return out
}
