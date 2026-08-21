package controller

import (
	"context"
	"testing"
	"time"

	"github.com/btj93/herdr-tokens/internal/config"
	"github.com/btj93/herdr-tokens/internal/herdrapi"
)

type fakeReporter struct {
	calls []string
	err   error
}

func (f *fakeReporter) ReportWorkspaceMetadata(_ context.Context, ws, _ string, _ map[string]*string, _ int64) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, ws)
	return nil
}

func ptr(s string) *string { return &s }

func snapWith(status *string) herdrapi.Snapshot {
	return herdrapi.Snapshot{
		Workspaces: []herdrapi.Workspace{{WorkspaceID: "w1", Label: "space-a", AgentStatus: status}},
	}
}

func TestFirstReconcileWrites(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	n, err := c.Reconcile(context.Background(), snapWith(ptr("idle")), time.Now())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1, nil", n, err)
	}
}

func TestUnchangedAndFreshIsSkipped(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snapWith(ptr("idle")), t0)
	n, _ := c.Reconcile(context.Background(), snapWith(ptr("idle")), t0.Add(5*time.Second))
	if n != 0 {
		t.Fatalf("wrote %d, want 0: unchanged and younger than ttl/3", n)
	}
	if len(r.calls) != 1 {
		t.Fatalf("%d writes, want 1", len(r.calls))
	}
}

// Steady state must still refresh the TTL, or tokens expire while healthy.
func TestUnchangedButStaleIsRewritten(t *testing.T) {
	r := &fakeReporter{}
	cfg := config.Default()
	c := New(cfg, r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snapWith(ptr("idle")), t0)
	n, _ := c.Reconcile(context.Background(), snapWith(ptr("idle")), t0.Add(cfg.HeartbeatAge()+time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: heartbeat past ttl/3", n)
	}
}

func TestChangeWritesImmediately(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snapWith(ptr("idle")), t0)
	n, _ := c.Reconcile(context.Background(), snapWith(ptr("working")), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: status changed", n)
	}
}

func TestClosedWorkspaceIsPruned(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snapWith(ptr("idle")), t0)
	c.Reconcile(context.Background(), herdrapi.Snapshot{}, t0.Add(time.Second))
	if c.Tracked() != 0 {
		t.Fatalf("tracked %d, want 0 after the workspace disappeared", c.Tracked())
	}
}

// A blip must not clear tokens; the TTL handles a genuinely dead daemon.
func TestWriteFailureIsNotRecordedAsSuccess(t *testing.T) {
	r := &fakeReporter{err: context.DeadlineExceeded}
	c := New(config.Default(), r)
	t0 := time.Now()
	if _, err := c.Reconcile(context.Background(), snapWith(ptr("idle")), t0); err == nil {
		t.Fatal("want error")
	}
	r.err = nil
	n, _ := c.Reconcile(context.Background(), snapWith(ptr("idle")), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: the failed write must not count as written", n)
	}
}
