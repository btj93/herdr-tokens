// Package daemon owns process lifecycle and the tick loop.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/btj93/herdr-tokens/internal/config"
	"github.com/btj93/herdr-tokens/internal/controller"
	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

const Version = "0.1.0"

// StateFile hashes the socket path into the filename so two named Herdr
// sessions cannot share a PID lock or log.
func StateFile(stateDir, socket, name string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(socket)))
	return filepath.Join(stateDir, hex.EncodeToString(sum[:8])+"-"+name)
}

func WritePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func ReadPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// Run polls on a single ticker until ctx ends. It subscribes to no events:
// the TTL refresh the daemon already owes doubles as the poll, which removes
// the metadata feedback loop, the pane.updated firehose, and per-pane
// subscription lifecycle entirely.
func Run(ctx context.Context, cfg config.Config, socket string) error {
	client := herdrapi.NewClient(socket)
	ctrl := controller.New(cfg, client)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	backoff := 250 * time.Millisecond
	const maxBackoff = 5 * time.Second

	// sawFailure tracks whether the most recent snapshot attempt(s) failed.
	// A Herdr restart always makes the socket disappear out from under us,
	// so it always produces at least one snapshot failure -- that is the
	// reliable signal that the process on the other end may have forgotten
	// everything it knew, even though our own write-skip cache has not.
	sawFailure := false

	for {
		callCtx, cancel := context.WithTimeout(ctx, cfg.PollInterval)
		snap, err := client.Snapshot(callCtx)
		cancel()

		if err != nil {
			// A zero-valued decode is an error, never an empty session, so we
			// log and skip rather than concluding there is nothing to do.
			log.Printf("snapshot failed, retrying in %v: %v", backoff, err)
			sawFailure = true
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 250 * time.Millisecond

		if sawFailure {
			// The first successful snapshot after one or more failures: Herdr
			// may have just restarted and lost its own in-memory workspace
			// metadata. Our cache doesn't know that on its own, so drop it
			// and let Reconcile write fresh, even for a token set that still
			// looks unchanged -- otherwise the sidebar would stay nameless
			// for up to HeartbeatAge() after Herdr is already back.
			ctrl.Invalidate()
			sawFailure = false
		}

		writeCtx, cancel := context.WithTimeout(ctx, cfg.PollInterval)
		if _, err := ctrl.Reconcile(writeCtx, snap, time.Now()); err != nil {
			log.Printf("reconcile: %v", err)
		}
		cancel()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// SocketPath resolves the Herdr socket, preferring the injected value.
func SocketPath() (string, error) {
	if s := os.Getenv("HERDR_SOCKET_PATH"); s != "" {
		return s, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(home, ".config", "herdr", "herdr.sock")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("daemon: HERDR_SOCKET_PATH unset and %s missing: %w", p, err)
	}
	return p, nil
}

// ErrAlreadyRunning is returned by AcquireLock when another process already
// holds the daemon lock for this socket.
var ErrAlreadyRunning = errors.New("daemon: already running")

// AcquireLock takes an exclusive, non-blocking advisory (flock) lock on
// path, creating the file if necessary. The OS releases the lock
// automatically when the holding process exits for ANY reason -- including
// SIGKILL and power loss -- which is exactly why the lock, not a PID file,
// is the liveness oracle here: a PID file can outlive its process (never
// cleaned up after a kill -9) and the PID it names can later be recycled by
// an unrelated process, but the lock cannot go "stale" the way a PID file
// can.
//
// On success the returned *os.File must be kept open for as long as the
// lock should be held; closing it (or process exit) releases it
// immediately. On contention it returns (nil, ErrAlreadyRunning).
func AcquireLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return f, nil
}
