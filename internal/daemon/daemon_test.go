package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Named Herdr sessions use different sockets and must not share a PID lock.
func TestStateFileIsKeyedBySocketPath(t *testing.T) {
	a := StateFile("/state", "/run/herdr-a.sock", "daemon.pid")
	b := StateFile("/state", "/run/herdr-b.sock", "daemon.pid")
	if a == b {
		t.Fatal("different sockets produced the same state file")
	}
	if !strings.HasPrefix(a, "/state/") || !strings.HasSuffix(a, "daemon.pid") {
		t.Fatalf("unexpected path %q", a)
	}
}

func TestStateFileIsStable(t *testing.T) {
	a := StateFile("/state", "/run/h.sock", "daemon.pid")
	b := StateFile("/state", "/run/h.sock", "daemon.pid")
	if a != b {
		t.Fatal("state file path is not deterministic")
	}
}

func TestPIDRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.pid")
	if err := WritePID(p); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	got, err := ReadPID(p)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if got != os.Getpid() {
		t.Fatalf("pid = %d, want %d", got, os.Getpid())
	}
}

func TestReadPIDMissingFile(t *testing.T) {
	if _, err := ReadPID(filepath.Join(t.TempDir(), "absent.pid")); err == nil {
		t.Fatal("want error for missing pid file")
	}
}
