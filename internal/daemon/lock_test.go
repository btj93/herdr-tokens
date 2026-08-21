package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain lets this same test binary act, when re-exec'd with a special
// env var, as a standalone process that does nothing but acquire the daemon
// lock and block forever. TestLockReleasesOnAbruptDeath uses this to prove
// the lock is released by a REAL OS-level process death (SIGKILL), not
// merely by an in-process file Close -- the two are not equivalent for
// crash scenarios like OOM-kill or power loss, which is exactly why the
// lock (not a PID file) is trustworthy as a liveness oracle.
func TestMain(m *testing.M) {
	if p := os.Getenv("HERDR_TOKENS_TEST_HOLD_LOCK"); p != "" {
		f, err := AcquireLock(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = f // keep referenced; must stay open for the lock to be held
		fmt.Println("locked")
		select {} // block until killed by the parent test
	}
	os.Exit(m.Run())
}

// TestAcquireLockOnlyOneWinner drives many goroutines through the exact
// acquire-or-bail path start() and runDaemon() use, all racing for the same
// lock file. Exactly one must win; every other must observe ErrAlreadyRunning,
// never a partial or corrupted state. This is the mechanism that fixes C1:
// concurrent `start` invocations (e.g. a startup hook racing a pane.created
// handler) can no longer both conclude "nothing is running" and both spawn a
// daemon, because the OS -- not an in-memory or file-content check -- serializes
// the race.
func TestAcquireLockOnlyOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	const n = 20

	var wg sync.WaitGroup
	var successes, contended, otherErrs int32
	files := make(chan *os.File, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := AcquireLock(path)
			switch {
			case err == nil:
				atomic.AddInt32(&successes, 1)
				files <- f
			case errors.Is(err, ErrAlreadyRunning):
				atomic.AddInt32(&contended, 1)
			default:
				atomic.AddInt32(&otherErrs, 1)
				t.Errorf("unexpected AcquireLock error: %v", err)
			}
		}()
	}
	wg.Wait()
	close(files)
	for f := range files {
		f.Close()
	}

	if otherErrs != 0 {
		t.Fatalf("%d unexpected errors, want 0", otherErrs)
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1 (of %d concurrent attempts, %d contended)", successes, n, contended)
	}
	if contended != n-1 {
		t.Fatalf("contended = %d, want %d", contended, n-1)
	}
}

// TestLockReleasesOnAbruptDeath proves the lock is not held hostage by a
// process that dies without a chance to clean up. It spawns a real child
// process that acquires the lock and blocks, confirms the lock is
// unavailable while the child is alive, SIGKILLs the child (no SIGTERM, no
// graceful shutdown -- simulating OOM-kill or `kill -9`), and confirms the
// lock becomes acquirable again immediately.
func TestLockReleasesOnAbruptDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "HERDR_TOKENS_TEST_HOLD_LOCK="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("child did not report holding the lock: line=%q err=%v", line, err)
	}

	// While the child is alive and holding the lock, we must NOT be able to
	// acquire it ourselves.
	if _, err := AcquireLock(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("AcquireLock while child alive: err=%v, want ErrAlreadyRunning", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL child: %v", err)
	}
	cmd.Wait()

	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		f, err := AcquireLock(path)
		if err == nil {
			f.Close()
			return
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("lock still unavailable after SIGKILL of holder: %v", lastErr)
}
