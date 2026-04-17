package config

import (
	"path/filepath"
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

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
