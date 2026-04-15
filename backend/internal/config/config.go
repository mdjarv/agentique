// Package config handles file-based configuration for Agentique.
//
// Config is loaded from <config-dir>/config.toml (~/.config/agentique/ on Linux).
// CLI flags take precedence over config file values.
// Missing config file is not an error — defaults apply.
package config

import (
	"errors"
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
