package main

import (
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/btj93/herdr-tokens/internal/daemon"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- string(buf)
	}()

	fn()
	w.Close()
	return <-done
}

// TestStopDoesNotSignalStaleUnrelatedPID proves C2 is fixed: a stale PID
// file naming a live but unrelated process must never be signalled when the
// daemon lock is free. A free lock means nothing is running for this
// socket, full stop -- the PID file's contents are irrelevant once that is
// established.
//
// Before the fix, stop() trusted the PID file alone and would have sent
// SIGTERM directly to whatever PID was recorded, with no identity check. To
// make the danger concrete rather than theoretical, this test writes a PID
// file naming a live, unrelated process -- this very test process, the safe
// choice suggested by the reviewer, since it is guaranteed alive for the
// test's duration. If the bug were still present, stop() would send SIGTERM
// to os.Getpid(), which -- left unhandled -- would kill the test binary
// itself. A SIGTERM handler is installed first so the assertion is "no
// signal observed" rather than "the process survived by accident".
func TestStopDoesNotSignalStaleUnrelatedPID(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "fake.sock")
	t.Setenv("HERDR_SOCKET_PATH", sock)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)

	pidPath := daemon.StateFile(dir, sock, "daemon.pid")
	if err := daemon.WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	// Deliberately do NOT acquire the daemon lock: nothing is "running".

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var code int
	out := captureStdout(t, func() { code = stop() })

	select {
	case <-sigCh:
		t.Fatal("stop() sent SIGTERM to this test process via a stale, unrelated PID -- C2 regression")
	case <-time.After(100 * time.Millisecond):
		// No signal received, as required.
	}

	if code != 0 {
		t.Fatalf("stop() exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "not running") {
		t.Fatalf("stop() output = %q, want to contain %q", out, "not running")
	}
}

// TestStopSignalsWhenLockIsHeld is the companion positive case: when a real
// daemon holds the lock, stop() must still find and signal it via the PID
// file, so the fix does not silently disable stop() altogether.
func TestStopSignalsWhenLockIsHeld(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "fake.sock")
	t.Setenv("HERDR_SOCKET_PATH", sock)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)

	lockPath := daemon.StateFile(dir, sock, "daemon.lock")
	lock, err := daemon.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer lock.Close()

	pidPath := daemon.StateFile(dir, sock, "daemon.pid")
	if err := daemon.WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var code int
	out := captureStdout(t, func() { code = stop() })

	select {
	case <-sigCh:
		// expected: the lock is held, so stop() must signal our own PID.
	case <-time.After(1 * time.Second):
		t.Fatal("stop() did not signal the process holding the lock")
	}

	if code != 0 {
		t.Fatalf("stop() exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("stop() output = %q, want to contain %q", out, "stopped")
	}
}
