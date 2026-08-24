package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/btj93/herdr-tokens/internal/config"
)

// --- scripted fake Herdr socket -------------------------------------------
//
// This is the harness Task 9 built to verify the T8-B ruling empirically: a
// unix socket that speaks Herdr's real wire protocol (one JSON line request,
// one JSON line response per connection, exactly one trailing '\n'), driving
// the REAL daemon.Run/run loop rather than a reimplementation of it. It was
// built once, used to produce a measurement (1 write vs 2), and then
// deliberately deleted as a temporary artifact -- the knowledge lived only
// in a report. This file promotes it permanently so the tick loop itself,
// not just its collaborators, has committed coverage.

// call is one recorded request the fake server received.
type call struct {
	method string
	at     time.Time
}

// callLog is a concurrency-safe recording of every request the fake server
// received, in arrival order.
type callLog struct {
	mu    sync.Mutex
	calls []call
}

func (l *callLog) record(method string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call{method, time.Now()})
	n := 0
	for _, c := range l.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

// times returns the arrival timestamps of every call to method, in order.
func (l *callLog) times(method string) []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []time.Time
	for _, c := range l.calls {
		if c.method == method {
			out = append(out, c.at)
		}
	}
	return out
}

func (l *callLog) count(method string) int { return len(l.times(method)) }

// countBetween reports how many calls to method arrived in (from, to].
func (l *callLog) countBetween(method string, from, to time.Time) int {
	n := 0
	for _, t := range l.times(method) {
		if t.After(from) && !t.After(to) {
			n++
		}
	}
	return n
}

// tokenStore is the fake server's memory of what workspace.report_metadata
// calls have actually applied, keyed by workspace ID -- i.e. what a REAL
// Herdr would echo back on the next session.snapshot. This is required for
// fidelity now that Controller.Reconcile compares desired tokens against
// the server's OWN reported state rather than this daemon's memory of what
// it wrote: a fake that never echoed writes back would make every tick
// look permanently "changed", which is not how the real protocol behaves,
// and would silently defeat any test asserting a steady state is skipped.
type tokenStore struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

// apply updates the stored tokens for workspaceID from one
// workspace.report_metadata call's decoded "tokens" param: a string value
// sets the key, a JSON null (decoded as a nil `any`) clears it -- mirroring
// herdrapi.Client.ReportWorkspaceMetadata, which sends a nil *string as
// JSON null specifically to clear a key.
func (s *tokenStore) apply(workspaceID string, tokens map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]map[string]string{}
	}
	cur := s.data[workspaceID]
	if cur == nil {
		cur = map[string]string{}
	}
	for k, v := range tokens {
		if v == nil {
			delete(cur, k)
			continue
		}
		if sv, ok := v.(string); ok {
			cur[k] = sv
		}
	}
	s.data[workspaceID] = cur
}

// snapshot returns what a session.snapshot response should currently report
// for workspaceID: nil (which marshals to JSON null) if nothing is set,
// matching herdrapi.Workspace.Tokens' nullable wire contract.
func (s *tokenStore) snapshot(workspaceID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.data[workspaceID]
	if len(cur) == 0 {
		return nil
	}
	out := make(map[string]string, len(cur))
	for k, v := range cur {
		out[k] = v
	}
	return out
}

// injectObservedTokens decodes a scripted session.snapshot reply and, for
// every workspace that does NOT already specify a "tokens" key, fills in
// what the store currently holds for it. A test's reply function therefore
// only needs to set "tokens" explicitly when it wants to simulate something
// the store would not produce on its own -- e.g. a Herdr restart wiping
// metadata with no snapshot failure, which is exactly the scenario
// TestRunWritesWhenObservedTokensVanishWithNoSnapshotFailure below drives.
// A reply with zero workspaces (the misread-envelope case) passes through
// with no workspaces to inject into, unchanged in effect.
func injectObservedTokens(raw string, store *tokenStore) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw // not this function's job to validate a deliberately malformed reply
	}
	result, _ := doc["result"].(map[string]any)
	snap, _ := result["snapshot"].(map[string]any)
	workspaces, _ := snap["workspaces"].([]any)
	for _, w := range workspaces {
		wsMap, ok := w.(map[string]any)
		if !ok {
			continue
		}
		if _, present := wsMap["tokens"]; present {
			continue
		}
		wsID, _ := wsMap["workspace_id"].(string)
		if got := store.snapshot(wsID); got != nil {
			wsMap["tokens"] = got
		} else {
			wsMap["tokens"] = nil
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return string(out)
}

// scriptedServer listens on a temp unix socket and speaks Herdr's real wire
// protocol. Each session.snapshot call is dispatched to reply with the
// 1-based call count for that method; reply must return the raw response
// body (no trailing newline -- the server appends the sole frame
// terminator). Before the response is sent, injectObservedTokens fills in
// any workspace's "tokens" key the reply itself left unset, from the
// server's own tokenStore -- see that function's doc comment. workspace.
// report_metadata calls always succeed; they are recorded in the call log
// as before, and now also applied to the tokenStore so later session.
// snapshot replies reflect them.
func scriptedServer(t *testing.T, reply func(n int) string) (string, *callLog) {
	t.Helper()
	log := &callLog{}
	store := &tokenStore{}
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
				method, _ := req["method"].(string)
				n := log.record(method)
				switch method {
				case "session.snapshot":
					c.Write([]byte(injectObservedTokens(reply(n), store) + "\n"))
				case "workspace.report_metadata":
					params, _ := req["params"].(map[string]any)
					wsID, _ := params["workspace_id"].(string)
					tokens, _ := params["tokens"].(map[string]any)
					store.apply(wsID, tokens)
					c.Write([]byte(`{"id":"1","result":{"type":"workspace_metadata_reported"}}` + "\n"))
				default:
					c.Write([]byte(`{"id":"1","error":{"code":"unknown_method","message":"nope"}}` + "\n"))
				}
			}(conn)
		}
	}()
	return sock, log
}

func validSnapshotReply(workspaceID, label, status string) string {
	return fmt.Sprintf(
		`{"id":"1","result":{"type":"session_snapshot","snapshot":{"workspaces":[{"workspace_id":%q,"label":%q,"agent_status":%q}],"agents":[]}}}`,
		workspaceID, label, status)
}

// misreadSnapshotReply is what a real Herdr misread looks like on the wire:
// a structurally valid envelope that decodes to zero workspaces. herdrapi's
// Client.Snapshot converts this into ErrEmptySnapshot -- Herdr always has at
// least one workspace, so this is never treated as a genuinely empty
// session.
const misreadSnapshotReply = `{"id":"1","result":{"type":"session_snapshot","snapshot":{"workspaces":[],"agents":[]}}}`

// --- 1. HAPPY PATH ---------------------------------------------------------

// TestRunHappyPathSkipsUnchangedFreshWrites drives several ticks of an
// unchanging, healthy snapshot through the real Run and asserts the
// write-skip rule holds through the full loop, not just in Reconcile
// isolation: the first tick writes, every later tick is unchanged and well
// inside the heartbeat window, so it is skipped.
func TestRunHappyPathSkipsUnchangedFreshWrites(t *testing.T) {
	sock, log := scriptedServer(t, func(n int) string {
		return validSnapshotReply("w1", "space-a", "idle")
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  30 * time.Millisecond,
		TTL:           10 * time.Second, // heartbeat age (TTL/3) far exceeds the test's runtime
		Value:         "label",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, cfg, sock)

	snaps := log.count("session.snapshot")
	writes := log.count("workspace.report_metadata")
	if snaps < 4 {
		t.Fatalf("only %d snapshot calls; test did not exercise enough ticks to be meaningful", snaps)
	}
	if writes != 1 {
		t.Fatalf("writes = %d over %d ticks, want exactly 1 (first tick only; every later tick unchanged and fresh)", writes, snaps)
	}
}

// --- 2. T8-B ----------------------------------------------------------------

// TestRunSkipsReconcileOnSnapshotError drives valid, misread, valid across
// three ticks and asserts the misread tick produces zero writes of its own.
// herdrapi.Client.Snapshot returns the zero-value Snapshot{} on ANY error
// (transport failure or the empty-workspaces decode this test uses).
//
// A write-count assertion alone cannot tell this guard apart from an absent
// one: Reconcile(Snapshot{}) always writes zero entries (its loop ranges
// over zero workspaces) whether or not it should have been called at all,
// and the already-shipped Invalidate-on-recovery feature independently
// clears the write-skip cache on the very next successful tick regardless.
// I confirmed this by literally removing the `continue` below -- see the
// report -- and it changed no write count anywhere. What DOES change,
// robustly, is retry PACING: `continue` is what sends a failed tick
// straight back to a fast, doubling backoff sleep; without it, the tick
// falls through to the bottom of the loop and waits out a full poll
// interval on top of the backoff sleep it just took, on every single
// failure. So this test drives several consecutive failures and asserts
// they arrive in quick, backoff-paced succession -- something the broken
// variant cannot do within the same short window -- while also confirming
// the more direct claim that no write ever lands during a failing tick.
func TestRunSkipsReconcileOnSnapshotError(t *testing.T) {
	const failures = 5 // n=2..6; n=1 and n=7 are valid
	sock, log := scriptedServer(t, func(n int) string {
		if n >= 2 && n <= 1+failures {
			return misreadSnapshotReply
		}
		return validSnapshotReply("w1", "space-a", "idle")
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  150 * time.Millisecond, // deliberately >> backoff, so a fallen-through
		TTL:           10 * time.Second,       // retry (bug) would be forced onto this much slower cadence
		Value:         "label",
	}
	base, max := 5*time.Millisecond, 20*time.Millisecond
	// Fast backoff-paced retries need on the close order of
	// base+2*base+4*base+4*base+4*base = 65ms to reach 7 calls; a tick that
	// fell through to the poll-interval wait even once would already blow
	// this budget (150ms for a single such tick alone).
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	run(ctx, cfg, sock, base, max)

	snapTimes := log.times("session.snapshot")
	if len(snapTimes) < 2+failures {
		t.Fatalf("only %d snapshot calls within 250ms, want at least %d: failed ticks are not retrying at the fast backoff pace, "+
			"suggesting a failure fell through past `continue` into the slow, poll-interval-paced path",
			len(snapTimes), 2+failures)
	}

	// The more direct claim: no write ever lands strictly inside any of the
	// failing ticks' own windows. snapTimes[j] is call number j+1, and call
	// (j+1)'s own write-processing window is (snapTimes[j], snapTimes[j+1]]
	// -- strictly after its own response, no later than the next call.
	// Misread calls are n=2..6, i.e. j=1..5.
	for j := 1; j <= failures; j++ {
		w := log.countBetween("workspace.report_metadata", snapTimes[j], snapTimes[j+1])
		if w != 0 {
			t.Fatalf("%d writes occurred during misread tick n=%d, want 0: Reconcile must not run on a snapshot error", w, j+1)
		}
	}
}

// --- 3. BACKOFF --------------------------------------------------------------

// TestRunBackoffDoublesCapsAndResets drives the unexported run with a tiny
// backoff schedule (so the whole sequence completes in well under a
// second, never sleeping anywhere near the real 250ms-5s schedule) through:
// four consecutive failures (to observe doubling, then the cap holding),
// one success (to trigger the reset), then two more failures (to prove the
// reset actually took hold rather than continuing from the capped value).
//
// Because real scheduling introduces jitter, assertions compare the RATIO
// between consecutive gaps rather than matching absolute durations.
func TestRunBackoffDoublesCapsAndResets(t *testing.T) {
	const (
		base = 10 * time.Millisecond
		max  = 40 * time.Millisecond // caps after two doublings: 10 -> 20 -> 40
		poll = 150 * time.Millisecond
	)
	sock, log := scriptedServer(t, func(n int) string {
		switch n {
		case 5: // first success, after four failures
			return validSnapshotReply("w1", "space-a", "idle")
		default:
			return misreadSnapshotReply
		}
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  poll,
		TTL:           10 * time.Second,
		Value:         "label",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	run(ctx, cfg, sock, base, max)

	ts := log.times("session.snapshot")
	if len(ts) < 7 {
		t.Fatalf("only %d snapshot calls, want at least 7 (4 failures, 1 success, 2 more failures)", len(ts))
	}

	gap := func(i, j int) time.Duration { return ts[j].Sub(ts[i]) }

	g1 := gap(0, 1) // n1->n2: base
	g2 := gap(1, 2) // n2->n3: 2x base
	g3 := gap(2, 3) // n3->n4: 4x base (== max, right at the cap)
	g4 := gap(3, 4) // n4->n5: capped, same as g3 (would be 8x base uncapped)
	g5 := gap(4, 5) // n5->n6: poll interval (n5 succeeded) -- NOT asserted on:
	// time.Ticker fires on an absolute schedule from when run started, not
	// "N since last read", so if the failure/backoff phase already ran past
	// a tick boundary the pending tick is delivered immediately. Its size
	// depends on exactly how the backoff phase's total duration lines up
	// against the poll interval and is not a reliable signal either way;
	// what matters for "reset" is g6 below, which is purely backoff-driven.
	g6 := gap(5, 6) // n6->n7: base again -- the reset

	if g2 < g1+g1/2 {
		t.Fatalf("gap2 (%v) is not clearly larger than gap1 (%v): doubling did not occur", g2, g1)
	}
	if g3 < g2+g2/2 {
		t.Fatalf("gap3 (%v) is not clearly larger than gap2 (%v): doubling did not continue", g3, g2)
	}
	if g4 > g3+g3/2 {
		t.Fatalf("gap4 (%v) grew past gap3 (%v): backoff did not cap", g4, g3)
	}
	if g6 > g1+g1 {
		t.Fatalf("gap6 (%v) did not reset to roughly the base backoff (gap1 = %v)", g6, g1)
	}
	if g6 > g4/2 {
		t.Fatalf("gap6 (%v) did not reset: still close to the pre-success capped gap4 (%v), want close to gap1 (%v)", g6, g4, g1)
	}
	t.Logf("gaps: g1=%v g2=%v g3=%v g4=%v g5(poll, informational)=%v g6=%v", g1, g2, g3, g4, g5, g6)
}

// --- 4. INVALIDATE-ON-RECOVERY ----------------------------------------------

// TestRunInvalidatesWriteSkipCacheOnRecovery drives valid, valid (unchanged
// -- must be skipped, proving Invalidate does NOT fire absent any failure),
// misread, valid (unchanged -- must nonetheless be rewritten, since a
// snapshot failure means Herdr may have restarted and forgotten its own
// metadata, which our cache cannot know on its own).
func TestRunInvalidatesWriteSkipCacheOnRecovery(t *testing.T) {
	sock, log := scriptedServer(t, func(n int) string {
		if n == 3 {
			return misreadSnapshotReply
		}
		return validSnapshotReply("w1", "space-a", "idle")
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  30 * time.Millisecond,
		TTL:           10 * time.Second,
		Value:         "label",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	run(ctx, cfg, sock, 10*time.Millisecond, 50*time.Millisecond)

	// A write resulting from processing snapshot call k (1-indexed) lands
	// strictly after call k's own timestamp and no later than call k+1's --
	// so tick k's write-window is (snapTimes[k-2], snapTimes[k-1]] in this
	// 0-indexed slice. Tick 4's window therefore needs snapTimes[4] (call 5)
	// as its upper bound, so at least 5 calls must have been observed.
	snapTimes := log.times("session.snapshot")
	if len(snapTimes) < 5 {
		t.Fatalf("only %d snapshot calls, want at least 5 (valid, valid, misread, valid, +1 to bound tick 4's window)", len(snapTimes))
	}

	tickTwoWrites := log.countBetween("workspace.report_metadata", snapTimes[1], snapTimes[2])
	if tickTwoWrites != 0 {
		t.Fatalf("tick 2 (unchanged, fresh, NO prior failure) wrote %d times, want 0: Invalidate must not fire on a clean run", tickTwoWrites)
	}

	tickFourWrites := log.countBetween("workspace.report_metadata", snapTimes[3], snapTimes[4])
	if tickFourWrites != 1 {
		t.Fatalf("tick 4 (unchanged, fresh, but preceded by a snapshot failure) wrote %d times, want 1: Invalidate must force a rewrite on recovery", tickFourWrites)
	}
}

// --- 5. SHUTDOWN -------------------------------------------------------------

// TestRunShutdownDuringTickerWaitIsPrompt cancels the context while Run is
// parked on the end-of-tick ticker wait (a healthy, quiescent daemon) and
// asserts it returns almost immediately rather than waiting out the
// (deliberately long) remainder of the poll interval.
func TestRunShutdownDuringTickerWaitIsPrompt(t *testing.T) {
	sock, _ := scriptedServer(t, func(n int) string {
		return validSnapshotReply("w1", "space-a", "idle")
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  2 * time.Second, // long: a slow return would be obvious
		TTL:           10 * time.Second,
		Value:         "label",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, sock) }()

	time.Sleep(50 * time.Millisecond) // let the first tick complete; Run is now on ticker.C
	start := time.Now()
	cancel()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("Run took %v to return after cancellation, want well under the 2s poll interval", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return within 500ms of context cancellation during the ticker wait")
	}
}

// --- 6. OBSERVED-TOKENS SELF-HEAL (parked item) -----------------------------

// TestRunWritesWhenObservedTokensVanishWithNoSnapshotFailure drives valid
// (write), valid-with-tokens-matching-what-was-just-written (skip, unchanged
// and fresh), valid-with-NO-tokens (write) across three ticks -- and EVERY
// session.snapshot call in this script succeeds. There is no misread and no
// transport error anywhere in it, so sawFailure never becomes true and
// Invalidate is never called.
//
// This is the sub-3s Herdr-restart scenario the parked defect names: a
// restart that completes inside one poll interval, and whose unavailable
// window misses this daemon's in-flight dial, produces no observable
// failure at all -- Invalidate's trigger never fires -- yet Herdr's own
// in-memory metadata is gone. If tick 3 still writes here, it can only be
// because Reconcile compared desired against ws.Tokens (the server's own
// report, which vanished) rather than against this Controller's memory of
// what it last wrote (which still thinks tick 2 is fresh and unchanged).
func TestRunWritesWhenObservedTokensVanishWithNoSnapshotFailure(t *testing.T) {
	sock, log := scriptedServer(t, func(n int) string {
		if n >= 3 {
			// Explicit override: the SERVER reports no tokens for this
			// workspace at all, even though nothing has failed -- this is
			// what injectObservedTokens leaves alone rather than filling
			// in from the store, simulating a Herdr restart that
			// completed inside one poll interval and never made an
			// in-flight dial fail.
			return `{"id":"1","result":{"type":"session_snapshot","snapshot":{"workspaces":[{"workspace_id":"w1","label":"space-a","agent_status":"idle","tokens":null}],"agents":[]}}}`
		}
		// n=1: nothing written yet, so the store fills in "tokens": null.
		// n=2: the store now reflects tick 1's actual write
		// (st_idle="space-a"), auto-filled in by injectObservedTokens --
		// this is "unchanged and fresh" as OBSERVED, not merely
		// remembered.
		return validSnapshotReply("w1", "space-a", "idle")
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  30 * time.Millisecond,
		TTL:           10 * time.Second, // heartbeat age (TTL/3) far exceeds the test's runtime
		Value:         "label",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, cfg, sock)

	snapTimes := log.times("session.snapshot")
	if len(snapTimes) < 4 {
		t.Fatalf("only %d snapshot calls, want at least 4 (write, skip, write, +1 to bound tick 3's window)", len(snapTimes))
	}

	tickOneWrites := log.countBetween("workspace.report_metadata", snapTimes[0], snapTimes[1])
	if tickOneWrites != 1 {
		t.Fatalf("tick 1 (first ever) wrote %d times, want 1", tickOneWrites)
	}

	tickTwoWrites := log.countBetween("workspace.report_metadata", snapTimes[1], snapTimes[2])
	if tickTwoWrites != 0 {
		t.Fatalf("tick 2 (server-observed tokens already match desired, fresh) wrote %d times, want 0", tickTwoWrites)
	}

	tickThreeWrites := log.countBetween("workspace.report_metadata", snapTimes[2], snapTimes[3])
	if tickThreeWrites != 1 {
		t.Fatalf("tick 3 (server-observed tokens vanished, NO snapshot failure, Invalidate never engaged) "+
			"wrote %d times, want 1: this is the sub-3s Herdr-restart case that must self-heal on ws.Tokens alone", tickThreeWrites)
	}
}

// TestRunShutdownDuringBackoffWaitIsPrompt cancels the context while Run is
// asleep in a backoff retry (an unhealthy daemon, mid-outage) and asserts it
// still returns promptly rather than waiting out the sleep.
func TestRunShutdownDuringBackoffWaitIsPrompt(t *testing.T) {
	sock, _ := scriptedServer(t, func(n int) string {
		return misreadSnapshotReply // always fails
	})
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion,
		PollInterval:  2 * time.Second,
		TTL:           10 * time.Second,
		Value:         "label",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, sock, 2*time.Second, 10*time.Second) }()

	time.Sleep(50 * time.Millisecond) // let the first failure be observed; Run is now asleep in backoff
	start := time.Now()
	cancel()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("Run took %v to return after cancellation, want well under the 2s backoff sleep", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return within 500ms of context cancellation during the backoff wait")
	}
}
