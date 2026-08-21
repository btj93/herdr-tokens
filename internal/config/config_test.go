package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultsMatchPublishedContract(t *testing.T) {
	c := Default()
	if c.PollInterval != 3*time.Second {
		t.Errorf("PollInterval = %v, want 3s", c.PollInterval)
	}
	if c.TTL != 90*time.Second {
		t.Errorf("TTL = %v, want 90s", c.TTL)
	}
	if c.Value != "label" {
		t.Errorf("Value = %q, want label", c.Value)
	}
	if c.HeartbeatAge() != 30*time.Second {
		t.Errorf("HeartbeatAge = %v, want 30s (ttl/3)", c.HeartbeatAge())
	}
}

func TestMissingFileIsValidAndUsesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c != Default() {
		t.Fatalf("Load = %+v, want defaults", c)
	}
}

func TestTTLMustBeAtLeastThreePolls(t *testing.T) {
	c := Default()
	c.PollInterval = 40 * time.Second
	c.TTL = 90 * time.Second // 90 < 3*40
	if err := c.Validate(); err == nil {
		t.Fatal("want error: one missed tick would expire a token")
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("schema_version = 1\ncolour = \"#ff0000\"\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("want error for unknown field (colours belong in herdr config)")
	}
}

func TestRejectsUnsupportedSchemaVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("schema_version = 99\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("want error for unsupported schema_version")
	}
}

func TestRejectsBadValueMode(t *testing.T) {
	c := Default()
	c.Value = "colour"
	if err := c.Validate(); err == nil {
		t.Fatal("want error for value mode other than label|status")
	}
}

func TestParsesValidFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("schema_version = 1\npoll_interval = \"5s\"\nttl = \"60s\"\nvalue = \"status\"\n"), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.PollInterval != 5*time.Second || c.TTL != 60*time.Second || c.Value != "status" {
		t.Fatalf("got %+v", c)
	}
}
