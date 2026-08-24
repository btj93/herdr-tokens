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

// snap builds a one-workspace snapshot. tokens is what the SERVER currently
// reports for that workspace (nil/absent means Herdr holds nothing for it),
// as distinct from anything this Controller remembers writing.
func snap(status *string, tokens map[string]string) herdrapi.Snapshot {
	return herdrapi.Snapshot{
		Workspaces: []herdrapi.Workspace{{WorkspaceID: "w1", Label: "space-a", AgentStatus: status, Tokens: tokens}},
	}
}

func TestFirstReconcileWrites(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	n, err := c.Reconcile(context.Background(), snap(ptr("idle"), nil), time.Now())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1, nil", n, err)
	}
}

// The Herdr-restart case with NO observed snapshot failure: this Controller's
// own bookkeeping says the write is unchanged and fresh, but the SERVER
// reports no tokens at all for the workspace (exactly what a restart that
// completes inside one poll interval, without an in-flight dial failing,
// looks like). Invalidate is never called here -- the fix must not depend
// on it.
func TestObservedEmptyForcesWriteDespiteFreshRememberedState(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: server reports no tokens even though our own cache thinks the write is unchanged and fresh", n)
	}
}

func TestObservedMatchesDesiredAndFreshIsSkipped(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), map[string]string{"st_idle": "space-a"}), t0.Add(5*time.Second))
	if n != 0 {
		t.Fatalf("wrote %d, want 0: server already holds exactly the desired tokens and the last write is younger than ttl/3", n)
	}
	if len(r.calls) != 1 {
		t.Fatalf("%d writes, want 1", len(r.calls))
	}
}

// Steady state must still refresh the TTL, or tokens expire while healthy,
// even when the server's copy already matches what we want.
func TestObservedMatchesDesiredButStaleIsRewritten(t *testing.T) {
	r := &fakeReporter{}
	cfg := config.Default()
	c := New(cfg, r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), map[string]string{"st_idle": "space-a"}), t0.Add(cfg.HeartbeatAge()+time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: heartbeat past ttl/3 even though the server already holds the desired tokens", n)
	}
}

func TestChangeWritesImmediately(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	// Server still holds exactly what we wrote for "idle". Our own derived
	// state now moves to "working" -- st_idle should clear and st_working
	// should be set -- which must still trigger a write.
	n, _ := c.Reconcile(context.Background(), snap(ptr("working"), map[string]string{"st_idle": "space-a"}), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: status changed, so desired no longer matches what the server holds", n)
	}
}

// Observed tokens differ from desired in exactly one key's VALUE (not
// presence/absence): the server holds a stale value for a key we still want
// set.
func TestObservedDiffersInOneKeyWrites(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), map[string]string{"st_idle": "stale-label"}), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: observed st_idle value differs from desired", n)
	}
}

// The server reports an EXTRA key beyond what we want -- a leftover we
// expect to be cleared (want[key] == nil) but the server still shows it
// present. This must trigger a write even though every key we DO want is
// already correct.
func TestObservedHasExtraKeyWrites(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	observed := map[string]string{"st_idle": "space-a", "st_working": "space-a"} // st_working should be cleared (nil) but the server still has it
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), observed), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: server still holds a stale st_working key that ought to be cleared", n)
	}
}

// A workspace reporting tokens: null (decoded by herdrapi as a nil map) must
// be treated exactly like a workspace with no matching key at all -- i.e.
// like every AllTokens key being absent -- and so forces a write when the
// desired set has anything non-nil. This is the same mechanism as
// TestObservedEmptyForcesWriteDespiteFreshRememberedState; asserted again
// here, named for the null-decode angle specifically, to keep the JSON
// contract's behaviour pinned even if that other test is ever changed.
func TestNullObservedTokensTreatedAsEmpty(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	var nullTokens map[string]string // simulates a decoded JSON `null`
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), nullTokens), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: nil (null-decoded) observed tokens must be treated as empty, forcing a write", n)
	}
}

// Invalidate is retained (see controller.go) even though it is no longer
// load-bearing for correctness -- this proves it still does what it always
// did, forcing a rewrite even when the server-observed tokens already match
// desired and the last write is fresh.
func TestInvalidateForcesRewriteEvenWhenObservedMatchesAndFresh(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
	c.Invalidate()
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), map[string]string{"st_idle": "space-a"}), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: Invalidate must force a rewrite even when observed already matches desired and is fresh", n)
	}
	if len(r.calls) != 2 {
		t.Fatalf("%d writes, want 2 (initial + post-Invalidate)", len(r.calls))
	}
}

func TestClosedWorkspaceIsPruned(t *testing.T) {
	r := &fakeReporter{}
	c := New(config.Default(), r)
	t0 := time.Now()
	c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0)
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
	if _, err := c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0); err == nil {
		t.Fatal("want error")
	}
	r.err = nil
	n, _ := c.Reconcile(context.Background(), snap(ptr("idle"), nil), t0.Add(time.Second))
	if n != 1 {
		t.Fatalf("wrote %d, want 1: the failed write must not count as written", n)
	}
}
