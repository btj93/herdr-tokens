// Package config loads the plugin's TOML configuration.
//
// Colours are deliberately NOT configurable here. They belong to the consumer
// (Herdr's own [ui.sidebar.spaces] rows); this plugin only decides which token
// name is written.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

const SchemaVersion = 1

type Config struct {
	SchemaVersion int
	PollInterval  time.Duration
	TTL           time.Duration
	Value         string
}

type fileShape struct {
	SchemaVersion *int    `toml:"schema_version"`
	PollInterval  *string `toml:"poll_interval"`
	TTL           *string `toml:"ttl"`
	Value         *string `toml:"value"`
}

// Default matches the values published to the herdr-tabline session.
// They are a public contract, not a private tuning choice.
func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		PollInterval:  3 * time.Second,
		TTL:           90 * time.Second,
		Value:         "label",
	}
}

// HeartbeatAge is how stale a successful write may become before it is
// rewritten even though nothing changed. ttl/3 tolerates two missed ticks.
func (c Config) HeartbeatAge() time.Duration { return c.TTL / 3 }

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("config: schema_version %d unsupported, want %d", c.SchemaVersion, SchemaVersion)
	}
	if c.PollInterval < 500*time.Millisecond || c.PollInterval > time.Minute {
		return fmt.Errorf("config: poll_interval %v out of range 500ms..60s", c.PollInterval)
	}
	if c.TTL > 24*time.Hour {
		return fmt.Errorf("config: ttl %v exceeds 24h", c.TTL)
	}
	if c.TTL < 3*c.PollInterval {
		return fmt.Errorf("config: ttl %v must be at least 3x poll_interval %v, "+
			"so one missed tick cannot expire a token", c.TTL, c.PollInterval)
	}
	if c.Value != "label" && c.Value != "status" {
		return fmt.Errorf("config: value %q must be \"label\" or \"status\"", c.Value)
	}
	return nil
}

// Load reads path. A missing file is valid and yields defaults.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	var f fileShape
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("config: unknown field %q in %s", undecoded[0].String(), path)
	}

	if f.SchemaVersion != nil {
		c.SchemaVersion = *f.SchemaVersion
	}
	if f.PollInterval != nil {
		if c.PollInterval, err = time.ParseDuration(*f.PollInterval); err != nil {
			return Config{}, fmt.Errorf("config: poll_interval: %w", err)
		}
	}
	if f.TTL != nil {
		if c.TTL, err = time.ParseDuration(*f.TTL); err != nil {
			return Config{}, fmt.Errorf("config: ttl: %w", err)
		}
	}
	if f.Value != nil {
		c.Value = *f.Value
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
