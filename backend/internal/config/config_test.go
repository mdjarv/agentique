package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadDevURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write a config with three dev-url slots.
	cfg := Default()
	cfg.DevURLs = []DevURLSlot{
		{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
		{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
		{Slot: "dev3", Port: 9212, PublicHost: "dev3.example.com"},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DevURLs) != 3 {
		t.Fatalf("want 3 dev-urls, got %d", len(got.DevURLs))
	}
	if got.DevURLs[0].Slot != "dev1" || got.DevURLs[0].Port != 9210 || got.DevURLs[0].PublicHost != "dev1.example.com" {
		t.Errorf("slot 0 wrong: %+v", got.DevURLs[0])
	}
}

func TestLoadBrainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// A [brain] section round-trips every brain setting: scheduled consolidation,
	// session-end models, and the full semantic-recall block.
	cfg := Default()
	cfg.Brain = BrainConfig{
		ConsolidateInterval: "6h", ConsolidateModel: "opus",
		LearnModel: "sonnet", OutcomeModel: "haiku",
		ChromaURL: "http://127.0.0.1:8000", EmbedURL: "http://127.0.0.1:11434/v1/embeddings",
		EmbedModel: "all-minilm", EmbedKey: "secret",
		SemanticThreshold: 0.42, VectorVeto: 0.05, Autocal: true, Recall: "off",
	}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Brain.ConsolidateInterval != "6h" || got.Brain.ConsolidateModel != "opus" {
		t.Fatalf("brain config did not round-trip: %+v", got.Brain)
	}
	if got.Brain.LearnModel != "sonnet" || got.Brain.OutcomeModel != "haiku" {
		t.Fatalf("brain session-end models did not round-trip: %+v", got.Brain)
	}
	if got.Brain != cfg.Brain {
		t.Fatalf("brain semantic settings did not round-trip:\n got %+v\nwant %+v", got.Brain, cfg.Brain)
	}

	// A config with no [brain] section leaves the fields empty (= scheduled consolidation
	// disabled), matching the pre-existing env-only behaviour.
	plain := filepath.Join(dir, "plain.toml")
	if err := Save(Default(), plain); err != nil {
		t.Fatal(err)
	}
	got2, err := Load(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Brain.ConsolidateInterval != "" || got2.Brain.ConsolidateModel != "" ||
		got2.Brain.LearnModel != nil || got2.Brain.OutcomeModel != nil {
		t.Fatalf("missing [brain] section should yield empty fields, got %+v", got2.Brain)
	}
}

func TestValidateDevURLs(t *testing.T) {
	cases := []struct {
		name    string
		slots   []DevURLSlot
		wantErr string
	}{
		{
			name:    "empty is fine",
			slots:   nil,
			wantErr: "",
		},
		{
			name: "valid",
			slots: []DevURLSlot{
				{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
				{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
			},
			wantErr: "",
		},
		{
			name:    "missing slot name",
			slots:   []DevURLSlot{{Port: 9210, PublicHost: "x.example.com"}},
			wantErr: "slot name",
		},
		{
			name:    "missing port",
			slots:   []DevURLSlot{{Slot: "dev1", PublicHost: "x.example.com"}},
			wantErr: "port",
		},
		{
			name:    "missing public host",
			slots:   []DevURLSlot{{Slot: "dev1", Port: 9210}},
			wantErr: "public-host",
		},
		{
			name: "duplicate slot",
			slots: []DevURLSlot{
				{Slot: "dev1", Port: 9210, PublicHost: "a.example.com"},
				{Slot: "dev1", Port: 9211, PublicHost: "b.example.com"},
			},
			wantErr: "duplicate slot",
		},
		{
			name: "duplicate port",
			slots: []DevURLSlot{
				{Slot: "dev1", Port: 9210, PublicHost: "a.example.com"},
				{Slot: "dev2", Port: 9210, PublicHost: "b.example.com"},
			},
			wantErr: "duplicate port",
		},
		{
			name: "duplicate host",
			slots: []DevURLSlot{
				{Slot: "dev1", Port: 9210, PublicHost: "a.example.com"},
				{Slot: "dev2", Port: 9211, PublicHost: "a.example.com"},
			},
			wantErr: "duplicate public-host",
		},
		{
			name:    "invalid port low",
			slots:   []DevURLSlot{{Slot: "dev1", Port: 0, PublicHost: "a.example.com"}},
			wantErr: "port",
		},
		{
			name:    "invalid port high",
			slots:   []DevURLSlot{{Slot: "dev1", Port: 70000, PublicHost: "a.example.com"}},
			wantErr: "port",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDevURLs(tc.slots)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("want no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("want error containing %q, got nil", tc.wantErr)
				return
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestAllRPOrigins(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "no dev-urls, only primary",
			cfg: Config{
				Server: ServerConfig{RPOrigin: "https://main.example.com"},
			},
			want: []string{"https://main.example.com"},
		},
		{
			name: "no dev-urls, no primary",
			cfg:  Config{},
			want: []string{},
		},
		{
			name: "several dev-urls plus primary",
			cfg: Config{
				Server: ServerConfig{RPOrigin: "https://main.example.com"},
				DevURLs: []DevURLSlot{
					{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
					{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
					{Slot: "dev3", Port: 9212, PublicHost: "dev3.example.com"},
				},
			},
			want: []string{
				"https://main.example.com",
				"https://dev1.example.com",
				"https://dev2.example.com",
				"https://dev3.example.com",
			},
		},
		{
			name: "dev-url host duplicates primary",
			cfg: Config{
				Server: ServerConfig{RPOrigin: "https://main.example.com"},
				DevURLs: []DevURLSlot{
					{Slot: "dev1", Port: 9210, PublicHost: "main.example.com"},
					{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
				},
			},
			want: []string{
				"https://main.example.com",
				"https://dev2.example.com",
			},
		},
		{
			name: "dev-urls only, no primary",
			cfg: Config{
				DevURLs: []DevURLSlot{
					{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
				},
			},
			want: []string{"https://dev1.example.com"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.AllRPOrigins()
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("AllRPOrigins() = %v, want %v", got, tc.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadClaudeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Claude = ClaudeConfig{ExcludeDynamicSystemPromptSections: true, AutoCompact: "200000"}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Claude != cfg.Claude {
		t.Fatalf("claude config did not round-trip:\n got %+v\nwant %+v", got.Claude, cfg.Claude)
	}

	// No [claude] section means both flags stay off, so the connector is built
	// exactly as it was before the section existed.
	plain := filepath.Join(dir, "plain.toml")
	if err := Save(Default(), plain); err != nil {
		t.Fatal(err)
	}
	got2, err := Load(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Claude != (ClaudeConfig{}) {
		t.Fatalf("missing [claude] section should yield zero config, got %+v", got2.Claude)
	}
}

func TestClaudeConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"unset leaves the CLI alone", "", false},
		{"auto", "auto", false},
		{"lower bound", "100000", false},
		{"upper bound", "1000000", false},
		{"below range", "99999", true},
		{"above range", "1000001", true},
		{"not a number", "loads", true},
		{"empty-ish garbage", " ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ClaudeConfig{AutoCompact: tt.val}.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tt.val, err, tt.wantErr)
			}
		})
	}
}

// A config carrying one of the four keys M2 retired still boots, WHATEVER TYPE it
// was written as. That is the contract's own words ("never refuse to boot"), and the
// spelling that mattered is `recall = false`: recall used to default on, so the key
// was a quoted string whose off switch was "false" — and the bool spelling anyone
// would reach for was a decode error that refused to start the server.
func TestRetiredBrainKeysNeverRefuseToBoot(t *testing.T) {
	spellings := map[string]string{
		"recall-bool":         "recall = false",
		"recall-string":       `recall = "false"`,
		"learn-model-string":  `learn-model = "sonnet"`,
		"learn-model-bool":    "learn-model = true",
		"outcome-model-empty": `outcome-model = ""`,
		"retry-max-int":       "retry-max = 3",
		"retry-max-string":    `retry-max = "3"`,
		"all-of-them":         "recall = false\nlearn-model = 1\noutcome-model = 2.5\nretry-max = true",
	}
	for name, line := range spellings {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("[brain]\nenabled = true\n"+line+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("a retired key must never refuse to boot, got: %v", err)
			}
			if !cfg.Brain.Enabled {
				t.Error("the rest of the section decoded wrong")
			}
		})
	}
}

// The warning that names a retired key tests presence, so a key that was written
// reads as carried and an absent one does not — whatever value it holds, the zero
// value of its type included.
func TestRetiredBrainKeyPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[brain]\nrecall = false\nretry-max = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brain.Recall == nil {
		t.Error("recall = false is a key the config carries")
	}
	if cfg.Brain.RetryMax == nil {
		t.Error("retry-max = 0 is a key the config carries")
	}
	if cfg.Brain.LearnModel != nil || cfg.Brain.OutcomeModel != nil {
		t.Error("a key nobody wrote must read as absent")
	}
}
