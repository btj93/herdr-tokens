package herdrapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// socketPathLimit guards well under the macOS AF_UNIX sun_path limit of 104
// bytes; see newSocketPath.
const socketPathLimit = 90

// newSocketPath returns a unique path inside a short private directory. A
// short root matters because Unix socket addresses are length-limited (104
// bytes on macOS), and a unique directory keeps a "missing" socket path from
// colliding with another process.
func newSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "herdrtok-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "socket")
	if len(sock) >= socketPathLimit {
		t.Fatalf("socket path %q is %d bytes, at or over the %d-byte guard (macOS sun_path limit is 104 bytes) -- shorten the temp dir prefix", sock, len(sock), socketPathLimit)
	}
	return sock
}

// fakeServer serves exactly one request per connection, as Herdr does.
func fakeServer(t *testing.T, reply func(req map[string]any) string) string {
	t.Helper()
	sock := newSocketPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close(); os.Remove(sock) })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req map[string]any
				json.Unmarshal(line, &req)
				c.Write([]byte(reply(req) + "\n"))
			}(conn)
		}
	}()
	return sock
}

// fakeServerBlocking accepts exactly one connection, reads the request, and
// then blocks (never writes a response, never closes) until the test
// cleans up. It simulates a stuck peer so a call cancelled via
// context.WithCancel (no deadline) can be proven to unblock promptly,
// rather than hanging on the read until something else acts.
func fakeServerBlocking(t *testing.T) string {
	t.Helper()
	sock := newSocketPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop); ln.Close(); os.Remove(sock) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		bufio.NewReader(conn).ReadBytes('\n')
		<-stop
	}()
	return sock
}

// compactFixture returns the on-disk fixture as compact (single-line) JSON,
// matching Herdr's real wire format: compact JSON, one response per
// connection, terminated by exactly one trailing '\n' that never appears
// earlier in the body (confirmed directly against the live server). The
// fixture on disk is pretty-printed for readability/diffing; splicing it in
// verbatim would make the fake server emit multi-line frames the real
// server never sends — a harness bug in the opposite direction from an
// inaccurate fixture, making the test MORE permissive than reality.
func compactFixture(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, fixtureBytes(t)); err != nil {
		t.Fatalf("compact fixture: %v", err)
	}
	if bytes.ContainsRune(buf.Bytes(), '\n') {
		t.Fatal("compacted fixture still contains a raw newline")
	}
	return buf.String()
}

func TestSnapshotUnwrapsNestedEnvelope(t *testing.T) {
	body := compactFixture(t)
	sock := fakeServer(t, func(req map[string]any) string {
		reply := `{"id":"1","result":` + body + `}`
		if n := strings.Count(reply, "\n"); n != 0 {
			t.Fatalf("reply contains %d embedded newlines before framing; want 0 (fakeServer appends the sole terminator)", n)
		}
		return reply
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := NewClient(sock).Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Workspaces) == 0 {
		t.Fatal("zero workspaces: envelope not unwrapped")
	}
}

func TestSnapshotRejectsZeroValuedDecode(t *testing.T) {
	sock := fakeServer(t, func(req map[string]any) string {
		return `{"id":"1","result":{"type":"session_snapshot","snapshot":{"workspaces":[],"agents":[]}}}`
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := NewClient(sock).Snapshot(ctx)
	if !errors.Is(err, ErrEmptySnapshot) {
		t.Fatalf("err = %v, want ErrEmptySnapshot", err)
	}
}

func TestSnapshotDecodesStringErrorCode(t *testing.T) {
	sock := fakeServer(t, func(req map[string]any) string {
		return `{"id":"1","error":{"code":"workspace_not_found","message":"nope"}}`
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := NewClient(sock).Snapshot(ctx)
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		t.Fatalf("err = %v, want *RPCError", err)
	}
	if rpc.Code != "workspace_not_found" {
		t.Fatalf("Code = %q", rpc.Code)
	}
}

func TestReportWorkspaceMetadataSendsNullsAndTTL(t *testing.T) {
	got := make(chan map[string]any, 1)
	sock := fakeServer(t, func(req map[string]any) string {
		got <- req
		return `{"id":"1","result":{"type":"workspace_metadata_reported"}}`
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	label := "space-a"
	tokens := map[string]*string{"st_working": &label, "st_idle": nil}
	if err := NewClient(sock).ReportWorkspaceMetadata(ctx, "w1", "herdr-tokens", tokens, 90000); err != nil {
		t.Fatalf("Report: %v", err)
	}
	req := <-got
	if req["method"] != "workspace.report_metadata" {
		t.Fatalf("method = %v", req["method"])
	}
	params := req["params"].(map[string]any)
	if params["ttl_ms"].(float64) != 90000 {
		t.Fatalf("ttl_ms = %v", params["ttl_ms"])
	}
	tk := params["tokens"].(map[string]any)
	if tk["st_working"] != "space-a" {
		t.Fatalf("st_working = %v", tk["st_working"])
	}
	v, ok := tk["st_idle"]
	if !ok || v != nil {
		t.Fatalf("st_idle = %v, want explicit null", v)
	}
}

// TestSnapshotCancelsWithoutDeadline proves that a context cancelled via
// context.WithCancel (no deadline) still unblocks an in-flight call
// promptly. SetDeadline alone only reacts to an explicit ctx.Deadline();
// without a watcher on ctx.Done(), a blocked read on a stuck peer would
// hang until the peer acted, cancellation or not.
func TestSnapshotCancelsWithoutDeadline(t *testing.T) {
	sock := fakeServerBlocking(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := NewClient(sock).Snapshot(ctx)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the call block on the read
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot did not return promptly after context cancellation without a deadline")
	}
}
