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

// captureOutput redirects both os.Stdout and os.Stderr for the duration of
// fn and returns whatever was written to each.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = wo, we
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() { b, _ := io.ReadAll(ro); doneOut <- string(b) }()
	go func() { b, _ := io.ReadAll(re); doneErr <- string(b) }()

	fn()
	wo.Close()
	we.Close()
	return <-doneOut, <-doneErr
}

// TestStartRejectsInvalidConfigWithoutSpawning proves FIX 1(a): start() must
// load and validate the config BEFORE spawning a child. Before this fix,
// start() never loaded the config at all -- only the freshly spawned child,
// inside runDaemon, discovered a bad config.toml, exited 2, and (with
// cmd.Stdout/Stderr wired to /dev/null) said so nowhere start() could see.
// start() printed "started (pid N)" and returned 0 regardless, for a child
// that was already dead. This test uses a temporary
// HERDR_PLUGIN_CONFIG_DIR/HERDR_SOCKET_PATH/HERDR_PLUGIN_STATE_DIR -- it
// never touches any real config or the live Herdr socket -- and asserts
// start() itself now reports the failure and never spawns anything.
func TestStartRejectsInvalidConfigWithoutSpawning(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "fake.sock")
	t.Setenv("HERDR_SOCKET_PATH", sock)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)

	configDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", configDir)
	badConfig := "poll_interval = \"not-a-duration\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(badConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var code int
	stdout, stderr := captureOutput(t, func() { code = start() })

	if code != 2 {
		t.Fatalf("start() exit code = %d, want 2 (same code runDaemon uses for an invalid config)", code)
	}
	if strings.Contains(stdout, "started") {
		t.Fatalf("start() stdout = %q, must not claim \"started\" for a config it rejected", stdout)
	}
	if !strings.Contains(stderr, "poll_interval") {
		t.Fatalf("start() stderr = %q, want it to contain the config error", stderr)
	}

	pidPath := daemon.StateFile(dir, sock, "daemon.pid")
	if _, err := os.Stat(pidPath); err == nil {
		t.Fatal("start() left behind a PID file for a daemon it should never have spawned")
	}
}

// TestDaemonLogPathIsAppendable is a unit-level check of the log
// redirection FIX 1(b) added to start(): the log file is opened with
// O_CREATE|O_WRONLY|O_APPEND at daemon.StateFile(stateDir, sock,
// "daemon.log") -- the exact call start() makes -- and repeated opens must
// append rather than truncate, or a second `start` after a crash would
// erase the previous run's diagnostics right when they're needed most.
//
// This deliberately does not drive start()'s actual spawn path: inside a
// `go test` binary, os.Executable() resolves to the test binary itself, so
// letting start() reach cmd.Start() here would recursively re-exec this
// entire test suite as a child (and that child's own copy of this same
// test would do it again). The cmd.Stdout/cmd.Stderr wiring to this file,
// and the log path appearing in the "started" message, are verified by
// inspection of start() instead (see cmd/herdr-tokens/main.go).
func TestDaemonLogPathIsAppendable(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "fake.sock")
	logPath := daemon.StateFile(dir, sock, "daemon.log")

	open := func() *os.File {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatalf("OpenFile: %v", err)
		}
		return f
	}

	f1 := open()
	if _, err := f1.WriteString("first run\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	f1.Close()

	f2 := open()
	if _, err := f2.WriteString("second run\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	f2.Close()

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if want := "first run\nsecond run\n"; string(got) != want {
		t.Fatalf("log contents = %q, want %q (second open must append, not truncate)", got, want)
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
