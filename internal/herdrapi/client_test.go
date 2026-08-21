package herdrapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeServer serves exactly one request per connection, as Herdr does.
func fakeServer(t *testing.T, reply func(req map[string]any) string) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "h.sock")
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

func TestSnapshotUnwrapsNestedEnvelope(t *testing.T) {
	body := string(fixtureBytes(t))
	sock := fakeServer(t, func(req map[string]any) string {
		return `{"id":"1","result":` + body + `}`
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
