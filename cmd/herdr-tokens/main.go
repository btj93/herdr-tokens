package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/btj93/herdr-tokens/internal/config"
	"github.com/btj93/herdr-tokens/internal/daemon"
	"github.com/btj93/herdr-tokens/internal/derive"
	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: herdr-tokens <start|stop|daemon|validate-config|preview|version>")
}

func configPath() string {
	if d := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "config.toml")
	}
	return "config.toml"
}

func stateDir() string {
	if d := os.Getenv("HERDR_PLUGIN_STATE_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(daemon.Version)
	case "validate-config":
		if _, err := config.Load(configPath()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("config ok")
	case "preview":
		os.Exit(preview())
	case "daemon":
		os.Exit(runDaemon())
	case "start":
		os.Exit(start())
	case "stop":
		os.Exit(stop())
	default:
		usage()
		os.Exit(2)
	}
}

func preview() int {
	cfg, err := config.Load(configPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	sock, err := daemon.SocketPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := herdrapi.NewClient(sock).Snapshot(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out := map[string]map[string]*string{}
	for _, ws := range snap.Workspaces {
		out[ws.Label] = derive.Desired(ws, snap.Agents, cfg.Value)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	return 0
}

func runDaemon() int {
	cfg, err := config.Load(configPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	sock, err := daemon.SocketPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// The lock, held for the entire process lifetime, is the sole liveness
	// oracle: if another daemon for this socket already holds it, that
	// daemon won the race and we quietly step aside rather than running a
	// second poller against the same workspaces.
	lockPath := daemon.StateFile(stateDir(), sock, "daemon.lock")
	lock, err := daemon.AcquireLock(lockPath)
	if err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer lock.Close()

	pidPath := daemon.StateFile(stateDir(), sock, "daemon.pid")
	if err := daemon.WritePID(pidPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.Remove(pidPath)

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	daemon.Run(ctx, cfg, sock)
	return 0
}

// start is idempotent: an existing live daemon for this socket is left
// alone. Liveness is decided by the lock, not the PID file -- a PID file can
// survive its process (never cleaned up after a kill -9) and its PID can be
// recycled by an unrelated process, so it can never safely answer "is a
// daemon running". The lock cannot go stale that way: the OS drops it the
// instant the holder exits, for any reason.
func start() int {
	sock, err := daemon.SocketPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	lockPath := daemon.StateFile(stateDir(), sock, "daemon.lock")
	lock, err := daemon.AcquireLock(lockPath)
	if err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			fmt.Println("already running")
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Nobody was running. Release immediately so the child we are about to
	// spawn can win the lock itself; holding it here would make the child's
	// own AcquireLock fail and cause it to step aside from a race that
	// doesn't exist.
	lock.Close()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("started (pid %d)\n", cmd.Process.Pid)
	return 0
}

// stop signals only the daemon belonging to this socket. It never signals
// based on the PID file alone: the file can name a live but unrelated
// process once its original PID has been recycled by the OS. The lock is
// consulted first -- if it can be acquired, nothing is running (a stale PID
// file, if any, is ignored and nothing is signalled); only lock contention
// (a real daemon holding it) leads to reading the PID file and signalling.
func stop() int {
	sock, err := daemon.SocketPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	lockPath := daemon.StateFile(stateDir(), sock, "daemon.lock")
	lock, err := daemon.AcquireLock(lockPath)
	if err == nil {
		lock.Close()
		fmt.Println("not running")
		return 0
	}
	if !errors.Is(err, daemon.ErrAlreadyRunning) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	pidPath := daemon.StateFile(stateDir(), sock, "daemon.pid")
	pid, err := daemon.ReadPID(pidPath)
	if err != nil {
		fmt.Println("not running")
		return 0
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("not running")
		return 0
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		fmt.Println("not running")
		return 0
	}
	fmt.Printf("stopped (pid %d)\n", pid)
	return 0
}
